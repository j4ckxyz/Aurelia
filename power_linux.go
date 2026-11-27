package main

import (
	"context"
	"log"
	"time"

	"github.com/godbus/dbus/v5"
)

// While music plays, the desktop is asked not to suspend the computer as
// it idles, as macOS and Windows do of themselves while sound plays.
// Closing the lid still suspends it: that is the user's to choose.

const (
	portalName     = "org.freedesktop.portal.Desktop"
	portalPath     = dbus.ObjectPath("/org/freedesktop/portal/desktop")
	portalInhibit  = "org.freedesktop.portal.Inhibit"
	inhibitSuspend = uint32(4)
)

// keepAwake asks the desktop, through its portal, not to suspend while
// music plays, and lets it again when it stops.
func (n *nowPlaying) keepAwake(on bool) {
	if on == (n.inhibit != "") || n.conn == nil {
		return
	}
	if !on {
		req := n.inhibit
		n.inhibit = ""
		if req != "pending" { // a pending one is closed as it comes
			go n.conn.Object(portalName, req).Call("org.freedesktop.portal.Request.Close", 0)
		}
		return
	}
	n.inhibit = "pending"
	conn := n.conn
	go func() {
		// A desktop without the portal, or a session without a desktop,
		// may never answer.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var req dbus.ObjectPath
		err := conn.Object(portalName, portalPath).CallWithContext(ctx, portalInhibit+".Inhibit", 0, "", inhibitSuspend,
			map[string]dbus.Variant{"reason": dbus.MakeVariant("Playing music")}).Store(&req)
		n.app.update(func() {
			switch {
			case err != nil:
				log.Print("keeping the computer awake: ", err)
				n.inhibit = ""
			case n.inhibit != "pending":
				// Stopped meanwhile.
				go conn.Object(portalName, req).Call("org.freedesktop.portal.Request.Close", 0)
			default:
				n.inhibit = req
			}
		})
	}()
}

// watchSleep tells the app as the computer goes to sleep and wakes,
// as logind says.
func watchSleep(a *App) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return
	}
	if err := conn.AddMatchSignal(dbus.WithMatchInterface("org.freedesktop.login1.Manager"), dbus.WithMatchMember("PrepareForSleep")); err != nil {
		conn.Close()
		return
	}
	ch := make(chan *dbus.Signal, 4)
	conn.Signal(ch)
	for sig := range ch {
		if len(sig.Body) == 1 {
			if sleeping, ok := sig.Body[0].(bool); ok {
				a.update(func() { a.jamSlept(sleeping) })
			}
		}
	}
}
