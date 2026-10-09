package audio

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/hajimehoshi/go-mp3"
	"github.com/mewkiz/flac/frame"
)

// decoder turns a file into interleaved stereo samples at the file's own
// rate. One goroutine uses it.
type decoder interface {
	// SampleRate is the rate of the frames Read returns.
	SampleRate() int
	// Len is the number of frames of the whole track, 0 when the file
	// does not say.
	Len() int64
	// Read decodes up to len(dst)/2 frames and returns how many; io.EOF
	// at the end of the track.
	Read(dst []float32) (int, error)
	// SeekFrame moves to a frame of the track.
	SeekFrame(frame int64) error
}

// openDecoder returns the decoder of what f holds, told by its first
// bytes: FLAC or MP3. hint is the number of frames per second of the
// track's duration that the library knows, for files that do not say.
func openDecoder(f *File, seconds float64) (decoder, error) {
	head := make([]byte, 10)
	if _, err := io.ReadFull(f, head); err != nil {
		return nil, fmt.Errorf("audio: reading the file: %w", err)
	}
	skip := int64(0)
	if string(head[:3]) == "ID3" {
		// An ID3v2 tag, which some FLAC files carry too.
		skip = 10 + int64(head[6]&0x7f)<<21 | int64(head[7]&0x7f)<<14 | int64(head[8]&0x7f)<<7 | int64(head[9]&0x7f)
		if _, err := f.Seek(skip, io.SeekStart); err != nil {
			return nil, err
		}
		if _, err := io.ReadFull(f, head[:4]); err != nil {
			return nil, fmt.Errorf("audio: reading the file: %w", err)
		}
	}
	switch {
	case string(head[:4]) == "fLaC":
		return newFLAC(f, skip, seconds)
	case skip > 0, head[0] == 0xff && head[1]&0xe0 == 0xe0:
		return newMP3(f, seconds)
	}
	return nil, fmt.Errorf("audio: a format Aurelia does not play (%q)", head[:4])
}

// flacDecoder decodes FLAC frame by frame, and seeks by searching the
// file for the frame of a sample, which needs no seek table and reads
// little of a file still downloading.
type flacDecoder struct {
	f  *File
	br *bufio.Reader

	rate, channels, bps int
	blockSize           int // of streams whose blocks are all one size, else 0
	total               int64
	seconds             float64
	dataStart           int64

	cur    *frame.Frame
	curPos int
	skip   int64 // frames to drop after a seek
}

func newFLAC(f *File, start int64, seconds float64) (*flacDecoder, error) {
	d := &flacDecoder{f: f, seconds: seconds}
	off := start + 4
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	// Metadata blocks: the stream's properties first, then others, which
	// are skipped unread, pictures included.
	var hdr [4]byte
	for first := true; ; first = false {
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			return nil, fmt.Errorf("audio: FLAC metadata: %w", err)
		}
		last, kind := hdr[0]&0x80 != 0, hdr[0]&0x7f
		size := int64(hdr[1])<<16 | int64(hdr[2])<<8 | int64(hdr[3])
		off += 4
		if first {
			if kind != 0 || size < 34 {
				return nil, errors.New("audio: FLAC without stream information")
			}
			var si [34]byte
			if _, err := io.ReadFull(f, si[:]); err != nil {
				return nil, fmt.Errorf("audio: FLAC metadata: %w", err)
			}
			minBlock, maxBlock := int(binary.BigEndian.Uint16(si[0:])), int(binary.BigEndian.Uint16(si[2:]))
			if minBlock == maxBlock {
				d.blockSize = maxBlock
			}
			x := binary.BigEndian.Uint64(si[10:])
			d.rate = int(x >> 44)
			d.channels = int(x>>41&7) + 1
			d.bps = int(x>>36&31) + 1
			d.total = int64(x & (1<<36 - 1))
			if d.rate == 0 {
				return nil, errors.New("audio: FLAC without a sample rate")
			}
		}
		off += size
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			return nil, err
		}
		if last {
			break
		}
	}
	d.dataStart = off
	d.br = bufio.NewReaderSize(f, 64<<10)
	return d, nil
}

func (d *flacDecoder) SampleRate() int { return d.rate }

func (d *flacDecoder) Len() int64 {
	if d.total > 0 {
		return d.total
	}
	return int64(d.seconds * float64(d.rate))
}

func (d *flacDecoder) Read(dst []float32) (int, error) {
	n, want := 0, len(dst)/2
	for n < want {
		if d.cur == nil || d.curPos >= int(d.cur.BlockSize) {
			f, err := frame.Parse(d.br)
			if err != nil {
				if n > 0 {
					return n, nil
				}
				if err == io.EOF || err == io.ErrUnexpectedEOF {
					return 0, io.EOF
				}
				return 0, err
			}
			d.cur, d.curPos = f, 0
			if len(f.Subframes) == 0 {
				d.cur = nil
			}
			continue
		}
		block := int(d.cur.BlockSize)
		if d.skip > 0 {
			k := min(int64(block-d.curPos), d.skip)
			d.curPos += int(k)
			d.skip -= k
			continue
		}
		k := min(block-d.curPos, want-n)
		bps := uint(d.cur.BitsPerSample)
		if bps == 0 {
			bps = uint(d.bps) // the frame leaves it to the stream
		}
		scale := 1 / float32(int64(1)<<(bps-1))
		l := d.cur.Subframes[0].Samples[d.curPos : d.curPos+k]
		r := l
		if len(d.cur.Subframes) > 1 {
			r = d.cur.Subframes[1].Samples[d.curPos : d.curPos+k]
		}
		out := dst[2*n : 2*(n+k)]
		for i := range l {
			out[2*i] = float32(l[i]) * scale
			out[2*i+1] = float32(r[i]) * scale
		}
		d.curPos += k
		n += k
	}
	return n, nil
}

func (d *flacDecoder) SeekFrame(target int64) error {
	d.cur, d.curPos, d.skip = nil, 0, 0
	if target <= 0 {
		return d.resume(d.dataStart, 0, 0)
	}
	size, err := d.f.Size()
	if err != nil {
		return err
	}
	total := d.Len()
	if total <= 0 {
		total = target * 2
	}
	// An interpolation search between the frame known at or before the
	// target and the offset known after it: FLAC's bit rate is even
	// enough that two or three probes find the frame, and none reads far
	// past it, which matters while the file downloads.
	lo, loSample := d.dataStart, int64(0)
	hi, hiSample := size, max(total, target+1)
	for i := 0; i < 24 && hi-lo > 48<<10 && target-loSample > int64(d.rate)/4; i++ {
		var guess int64
		if i%3 == 2 {
			guess = lo + (hi-lo)/2 // interpolation may creep: halve now and then
		} else {
			frac := float64(target-loSample) / float64(hiSample-loSample)
			guess = lo + int64(frac*float64(hi-lo)) - 24<<10
		}
		guess = min(max(guess, lo+1), hi-1)
		off, sample, ok, err := d.findFrame(guess, hi)
		if err != nil {
			return err
		}
		switch {
		case !ok:
			hi = guess
		case sample <= target:
			lo, loSample = off, sample
		default:
			hi, hiSample = guess, sample
		}
	}
	return d.resume(lo, loSample, target)
}

// resume decodes from the frame at off, whose first sample is sample,
// toward target.
func (d *flacDecoder) resume(off, sample, target int64) error {
	if _, err := d.f.Seek(off, io.SeekStart); err != nil {
		return err
	}
	d.br.Reset(d.f)
	d.skip = max(target-sample, 0)
	return nil
}

// findFrame finds the first frame starting at or after from, before
// limit, and returns its offset and its first sample.
func (d *flacDecoder) findFrame(from, limit int64) (off, sample int64, ok bool, err error) {
	if _, err := d.f.Seek(from, io.SeekStart); err != nil {
		return 0, 0, false, err
	}
	const chunk = 32 << 10
	var buf []byte
	scanned := 0
	for len(buf) < 1<<20 && from+int64(len(buf)) < limit {
		n := int(min(chunk, limit-from-int64(len(buf))))
		start := len(buf)
		buf = append(buf, make([]byte, n)...)
		got, rerr := io.ReadFull(d.f, buf[start:])
		buf = buf[:start+got]
		for i := scanned; i+16 <= len(buf); i++ {
			if buf[i] != 0xff || buf[i+1]&0xfe != 0xf8 {
				continue
			}
			// A frame's header ends with a CRC of itself, which chance
			// matches once in 256: its properties must be the stream's.
			f, herr := frame.New(bytes.NewReader(buf[i:]))
			if herr != nil || f.Channels.Count() != d.channels ||
				(f.BitsPerSample != 0 && int(f.BitsPerSample) != d.bps) ||
				(f.SampleRate != 0 && int(f.SampleRate) != d.rate) {
				continue
			}
			s := int64(f.Num)
			if f.HasFixedBlockSize {
				if d.blockSize == 0 {
					continue
				}
				s *= int64(d.blockSize)
			}
			return from + int64(i), s, true, nil
		}
		scanned = max(len(buf)-15, 0)
		if rerr != nil {
			if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
				break
			}
			return 0, 0, false, rerr
		}
	}
	return 0, 0, false, nil
}

// mp3Decoder decodes MP3. go-mp3 reads a whole file to seek in it, so the
// file plays as a stream at first, which starts at once, and opens anew
// for the first seek, which waits for the download to end.
type mp3Decoder struct {
	f        *File
	dec      *mp3.Decoder
	seekable bool
	seconds  float64
	buf      []byte
}

// readerOnly hides the Seek of a file.
type readerOnly struct{ r io.Reader }

func (r readerOnly) Read(p []byte) (int, error) { return r.r.Read(p) }

func newMP3(f *File, seconds float64) (*mp3Decoder, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	dec, err := mp3.NewDecoder(readerOnly{bufio.NewReaderSize(f, 64<<10)})
	if err != nil {
		return nil, fmt.Errorf("audio: MP3: %w", err)
	}
	return &mp3Decoder{f: f, dec: dec, seconds: seconds}, nil
}

func (d *mp3Decoder) SampleRate() int { return d.dec.SampleRate() }

func (d *mp3Decoder) Len() int64 {
	if d.seekable {
		if n := d.dec.Length(); n > 0 {
			return n / 4
		}
	}
	return int64(d.seconds * float64(d.dec.SampleRate()))
}

func (d *mp3Decoder) Read(dst []float32) (int, error) {
	want := len(dst) / 2 * 4
	if cap(d.buf) < want {
		d.buf = make([]byte, want)
	}
	buf := d.buf[:want]
	n, err := io.ReadFull(d.dec, buf)
	n -= n % 4
	for i := 0; i < n/2; i++ {
		dst[i] = float32(int16(binary.LittleEndian.Uint16(buf[2*i:]))) / 32768
	}
	if n > 0 {
		return n / 4, nil
	}
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return 0, err
}

func (d *mp3Decoder) SeekFrame(target int64) error {
	if !d.seekable {
		if _, err := d.f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		dec, err := mp3.NewDecoder(d.f)
		if err != nil {
			return fmt.Errorf("audio: MP3: %w", err)
		}
		d.dec, d.seekable = dec, true
	}
	_, err := d.dec.Seek(max(target, 0)*4, io.SeekStart)
	return err
}
