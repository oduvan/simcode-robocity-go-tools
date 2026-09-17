package main

import "os"

func readIfExists(name string) ([]byte, error) {
	b, err := os.ReadFile(name)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}
