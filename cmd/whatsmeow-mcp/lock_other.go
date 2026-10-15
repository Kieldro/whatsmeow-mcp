//go:build !unix

package main

// lockDir is a no-op on platforms without flock.
func lockDir(string) (func(), error) { return func() {}, nil }
