//go:build !linux

package main

import "errors"

// sandbox is Linux's, where Landlock is.
func sandbox(readable []string, connectPorts []string) (string, error) {
	return "", errors.New("there is no sandbox on this system")
}
