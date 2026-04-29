package server

import "os"

func userHomeImpl() (string, error) {
	return os.UserHomeDir()
}
