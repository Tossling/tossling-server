package main

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const projectUserPrefix = "project-"

var projectTopic = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)

func runProject(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: tossling-server project add <channel> [name] [--publisher <name>] | list | remove <channel> [flags]")
	}
	action, rest := args[0], args[1:]
	var positional, flags []string
	publisher := ""
	for i := 0; i < len(rest); i++ {
		switch {
		case rest[i] == "--publisher" && i+1 < len(rest):
			publisher = rest[i+1]
			i++
		case strings.HasPrefix(rest[i], "--publisher="):
			publisher = strings.TrimPrefix(rest[i], "--publisher=")
		case strings.HasPrefix(rest[i], "-"):
			flags = append(flags, rest[i:]...)
			i = len(rest)
		default:
			positional = append(positional, rest[i])
		}
	}
	opts := parseOptions(flags)
	settings, _, err := newSettingsStore(opts.dataDir).load()
	if err != nil {
		return err
	}
	manager, err := openUserManager(opts.dataDir)
	if err != nil {
		return err
	}
	defer manager.Close()
	service := newServiceOverHTTP(manager, opts, settings.Token)
	switch action {
	case "add":
		if len(positional) == 0 {
			return errors.New("usage: tossling-server project add <channel> [name]")
		}
		topic, name := positional[0], strings.Join(positional[1:], " ")
		if publisher == "" {
			publisher = topic
		}
		token, err := service.add(topic, name, publisher)
		if err != nil {
			return err
		}
		if name == "" {
			name = topic
		}
		if token == "" {
			fmt.Printf("Project %q on channel %s. %s publishes there with the token it already has.\n", name, topic, publisher)
			return nil
		}
		fmt.Printf("Project %q on channel %s, publisher %s. Its token can only write to the channels of %s:\n\n", name, topic, publisher, publisher)
		fmt.Printf("    %s\n\n", token)
		fmt.Printf("    %s\n", curlExample(opts.baseURL, topic, token))
		return nil
	case "list":
		projects, err := service.projects()
		if err != nil {
			return err
		}
		for _, p := range projects {
			if p.Publisher != "" {
				fmt.Printf("%-24s %-24s publisher %s\n", p.Topic, p.Name, p.Publisher)
			}
		}
		return nil
	case "remove":
		if len(positional) != 1 {
			return errors.New("usage: tossling-server project remove <channel>")
		}
		if _, err := service.project(positional[0]); err != nil {
			return err
		}
		if err := service.remove(positional[0]); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
		fmt.Printf("Project %s removed: nobody can publish there any more.\n", positional[0])
		return nil
	}
	return fmt.Errorf("unknown project action %q", action)
}

func curlExample(baseURL, topic, token string) string {
	return fmt.Sprintf(`curl -H "Authorization: Bearer %s" -H "Title: Build finished" -d "assembleRelease in 3 min" %s/%s`, token, baseURL, topic)
}
