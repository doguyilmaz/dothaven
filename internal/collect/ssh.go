package collect

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

const sshRedactionMarker = "[REDACTED]"

var (
	sshHostRe     = regexp.MustCompile(`(?i)^Host\s+(.+)`)
	sshHostNameRe = regexp.MustCompile(`(?i)^HostName\s+(.+)`)
	sshIdentityRe = regexp.MustCompile(`(?i)^IdentityFile\s+(.+)`)
)

// SSHHost is a single parsed entry from ~/.ssh/config.
type SSHHost struct {
	Host         string
	HostName     string
	IdentityFile string
}

// ParseSSHConfig parses an ssh config into ordered host entries. A new entry
// begins at each Host line; HostName/IdentityFile attach to the current entry.
func ParseSSHConfig(content string) []SSHHost {
	var hosts []SSHHost
	var current *SSHHost

	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if m := sshHostRe.FindStringSubmatch(trimmed); m != nil {
			if current != nil && current.Host != "" {
				hosts = append(hosts, *current)
			}
			current = &SSHHost{Host: strings.TrimSpace(m[1])}
			continue
		}

		if current == nil {
			continue
		}

		if m := sshHostNameRe.FindStringSubmatch(trimmed); m != nil {
			current.HostName = strings.TrimSpace(m[1])
		}

		if m := sshIdentityRe.FindStringSubmatch(trimmed); m != nil {
			current.IdentityFile = strings.TrimSpace(m[1])
		}
	}

	if current != nil && current.Host != "" {
		hosts = append(hosts, *current)
	}
	return hosts
}

// SSHCollector reads ~/.ssh/config and emits the "ssh.hosts" section with items
// columned [host, hostname, identity]. HostName and IdentityFile are redacted
// when c.Redact is set.
func SSHCollector(c Ctx) snapshot.Snapshot {
	out := snapshot.Snapshot{}

	configPath := filepath.Join(c.Home, ".ssh", "config")
	data, err := c.Env.ReadFile(configPath)
	if err != nil {
		return out
	}

	hosts := ParseSSHConfig(string(data))
	if len(hosts) == 0 {
		return out
	}

	items := make([]snapshot.Item, len(hosts))
	for i, h := range hosts {
		hn := h.HostName
		id := h.IdentityFile
		if c.Redact {
			hn = sshRedactionMarker
			id = sshRedactionMarker
		}
		items[i] = snapshot.Item{
			Raw:     h.Host + " | " + hn + " | " + id,
			Columns: []string{h.Host, hn, id},
		}
	}

	out["ssh.hosts"] = snapshot.Section{Items: items}
	return out
}
