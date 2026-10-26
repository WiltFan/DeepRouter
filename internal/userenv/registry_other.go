//go:build !windows

package userenv

func platformDefault() Store { return nil }
