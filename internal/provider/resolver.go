package provider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// commandResolver supplements PATH lookup with the standard installation
// locations used by package managers. Desktop apps launched from Finder,
// Explorer, or a Linux application menu often receive a smaller PATH than an
// interactive terminal.
type commandResolver struct {
	lookPath lookPathFunc
	homeDir  func() (string, error)
	getenv   func(string) string
	goos     string
}

func newCommandResolver() commandResolver {
	return commandResolver{
		lookPath: exec.LookPath,
		homeDir:  os.UserHomeDir,
		getenv:   os.Getenv,
		goos:     runtime.GOOS,
	}
}

func (r commandResolver) Resolve(command string) (string, error) {
	if path, err := r.lookPath(command); err == nil {
		return path, nil
	}

	home, _ := r.homeDir()
	for _, candidate := range commandCandidates(command, r.goos, home, r.getenv) {
		if isExecutable(candidate, r.goos) {
			return candidate, nil
		}
	}

	for _, pattern := range commandPatterns(command, r.goos, home) {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		// Version-manager directories are normally ordered oldest first. Prefer
		// the most recently named installation when more than one is present.
		sort.Sort(sort.Reverse(sort.StringSlice(matches)))
		for _, candidate := range matches {
			if isExecutable(candidate, r.goos) {
				return candidate, nil
			}
		}
	}

	return "", fmt.Errorf("%s: %w", command, exec.ErrNotFound)
}

func commandCandidates(command, goos, home string, getenv func(string) string) []string {
	directories := make([]string, 0, 16)
	addDirectory := func(path string) {
		if path == "" {
			return
		}
		for _, existing := range directories {
			if existing == path {
				return
			}
		}
		directories = append(directories, path)
	}

	switch goos {
	case "windows":
		if appData := getenv("APPDATA"); appData != "" {
			addDirectory(filepath.Join(appData, "npm"))
		}
		if localAppData := getenv("LOCALAPPDATA"); localAppData != "" {
			addDirectory(filepath.Join(localAppData, "pnpm"))
		}
		if home != "" {
			addDirectory(filepath.Join(home, "AppData", "Roaming", "npm"))
			addDirectory(filepath.Join(home, ".volta", "bin"))
			addDirectory(filepath.Join(home, ".local", "bin"))
		}
	case "darwin":
		addDirectory("/opt/homebrew/bin")
		addDirectory("/usr/local/bin")
		if home != "" {
			addDirectory(filepath.Join(home, ".local", "bin"))
			addDirectory(filepath.Join(home, "bin"))
			addDirectory(filepath.Join(home, ".volta", "bin"))
			addDirectory(filepath.Join(home, "Library", "pnpm"))
			addDirectory(filepath.Join(home, ".npm-global", "bin"))
		}
	default:
		addDirectory("/usr/local/bin")
		addDirectory("/usr/bin")
		addDirectory("/snap/bin")
		if home != "" {
			addDirectory(filepath.Join(home, ".local", "bin"))
			addDirectory(filepath.Join(home, "bin"))
			addDirectory(filepath.Join(home, ".volta", "bin"))
			addDirectory(filepath.Join(home, ".npm-global", "bin"))
			addDirectory(filepath.Join(home, ".local", "share", "pnpm"))
		}
	}

	names := []string{command}
	if goos == "windows" && filepath.Ext(command) == "" {
		names = []string{command + ".exe", command + ".cmd", command + ".bat", command}
	}

	candidates := make([]string, 0, len(directories)*len(names))
	for _, directory := range directories {
		for _, name := range names {
			candidates = append(candidates, filepath.Join(directory, name))
		}
	}
	return candidates
}

func commandPatterns(command, goos, home string) []string {
	if home == "" {
		return nil
	}

	if goos == "windows" {
		return []string{
			filepath.Join(home, ".nvm", "*", command+".exe"),
			filepath.Join(home, ".nvm", "*", command+".cmd"),
		}
	}

	return []string{
		filepath.Join(home, ".nvm", "versions", "node", "*", "bin", command),
		filepath.Join(home, ".local", "share", "fnm", "node-versions", "*", "installation", "bin", command),
	}
}

func isExecutable(path, goos string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return goos == "windows" || info.Mode().Perm()&0o111 != 0
}

func environmentWithExecutableDir(environ []string, executablePath string) []string {
	directory := filepath.Dir(executablePath)
	if directory == "." || directory == "" {
		return environ
	}

	result := append([]string(nil), environ...)
	for index, entry := range result {
		key, value, found := strings.Cut(entry, "=")
		if found && strings.EqualFold(key, "PATH") {
			result[index] = key + "=" + directory + string(os.PathListSeparator) + value
			return result
		}
	}
	return append(result, "PATH="+directory)
}
