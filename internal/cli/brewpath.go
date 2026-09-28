package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/sys"
)

// brewPrefixes are where Homebrew lives: Apple Silicon, Intel Macs, Linux.
var brewPrefixes = []string{"/opt/homebrew", "/usr/local", "/home/linuxbrew/.linuxbrew"}

// shellStartupFiles are the files a shell runs at start, where the line that
// puts Homebrew on the PATH lives.
var shellStartupFiles = []string{".zprofile", ".zshrc", ".zshenv", ".bash_profile", ".bashrc", ".profile", ".config/fish/config.fish"}

// brewRef is a startup file that runs Homebrew from where it is not.
type brewRef struct{ File, Wrong, Right string }

// staleBrewRefs finds startup files that call brew under a prefix this
// machine does not have, while it does have Homebrew elsewhere. This is common
// after moving from an Intel Mac to Apple Silicon (or back): every new shell
// starts with "no such file or directory: /usr/local/bin/brew". Pure: files
// maps a name to its content, and exists says whether a path is here.
func staleBrewRefs(files map[string]string, exists func(string) bool) []brewRef {
	here := ""
	for _, p := range brewPrefixes {
		if exists(p + "/bin/brew") {
			here = p
			break
		}
	}
	if here == "" {
		return nil // no Homebrew here yet: reinstall's job, not a path problem
	}
	var out []brewRef
	for _, name := range shellStartupFiles {
		content, ok := files[name]
		if !ok {
			continue
		}
		for _, p := range brewPrefixes {
			if p != here && strings.Contains(content, p+"/bin/brew") && !exists(p+"/bin/brew") {
				out = append(out, brewRef{File: "~/" + name, Wrong: p, Right: here})
				break
			}
		}
	}
	return out
}

// checkBrewPaths reads this machine's startup files for staleBrewRefs.
func checkBrewPaths(env *sys.OS) []brewRef {
	files := map[string]string{}
	for _, name := range shellStartupFiles {
		if b, err := os.ReadFile(filepath.Join(env.Home(), name)); err == nil {
			files[name] = string(b)
		}
	}
	return staleBrewRefs(files, func(p string) bool {
		_, err := os.Stat(p)
		return err == nil
	})
}

// printBrewPaths warns about startup files that call Homebrew from the wrong
// place, with the fix.
func printBrewPaths(refs []brewRef) {
	for _, r := range refs {
		fmt.Printf("\n%s %s runs Homebrew from %s, but on this machine it is in %s.\n",
			warn("⚠"), r.File, r.Wrong, r.Right)
		fmt.Printf("  Every new shell will start with an error until it says %s instead.\n", r.Right+"/bin/brew")
	}
}
