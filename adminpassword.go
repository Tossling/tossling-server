package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

func runAdminPassword(args []string) error {
	opts := parseOptions(args)
	store := newSettingsStore(opts.dataDir)
	if _, _, err := store.load(); err != nil {
		return err
	}
	var password string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "New panel password: ")
		first, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		fmt.Fprint(os.Stderr, "Repeat it: ")
		second, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		if string(first) != string(second) {
			return errors.New("the passwords do not match")
		}
		password = string(first)
	} else {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return errors.New("no password on stdin")
		}
		password = strings.TrimRight(line, "\r\n")
	}
	if err := store.setAdminPassword(password); err != nil {
		return err
	}
	fmt.Printf("The panel password is set; open %s/admin\n", opts.baseURL)
	return nil
}
