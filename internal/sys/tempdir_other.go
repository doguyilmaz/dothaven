//go:build !unix

package sys

// Without a way to ask about a process, every folder is assumed in use.
func processAlive(int) bool { return true }

func ownedByMe(string) bool { return false }
