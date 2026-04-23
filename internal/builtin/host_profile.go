package builtin

import (
	"strings"

	"osagentmvp/internal/models"
)

func hostProfileCommand() string {
	return strings.TrimSpace(`
hostname_value=$(hostname 2>/dev/null || true)
kernel_value=$(uname -a 2>/dev/null || true)
distro_value=$((. /etc/os-release && printf '%s' "$PRETTY_NAME") 2>/dev/null || sw_vers -productVersion 2>/dev/null || true)
shell_value=${SHELL:-}
user_value=$(whoami 2>/dev/null || true)
home_value=${HOME:-}
cwd_value=$(pwd 2>/dev/null || true)
path_value=${PATH:-}
init_value=$(ps -p 1 -o comm= 2>/dev/null || true)
capabilities_value=""
for cmd in systemctl service journalctl ss netstat apt dnf yum zypper pacman python3 git; do
  if command -v "$cmd" >/dev/null 2>&1; then
    if [ -n "$capabilities_value" ]; then
      capabilities_value="$capabilities_value,$cmd"
    else
      capabilities_value="$cmd"
    fi
  fi
done
printf 'HOSTNAME=%s\n' "$hostname_value"
printf 'KERNEL=%s\n' "$kernel_value"
printf 'DISTRO=%s\n' "$distro_value"
printf 'SHELL=%s\n' "$shell_value"
printf 'USER=%s\n' "$user_value"
printf 'HOME=%s\n' "$home_value"
printf 'CWD=%s\n' "$cwd_value"
printf 'PATH=%s\n' "$path_value"
printf 'INIT=%s\n' "$init_value"
printf 'CAPABILITIES=%s\n' "$capabilities_value"
`)
}

func parseHostProfile(stdout, stderr string) models.HostProfile {
	profile := models.HostProfile{}
	for _, line := range strings.Split(stdout, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "HOSTNAME":
			profile.Hostname = value
		case "KERNEL":
			profile.Kernel = value
		case "DISTRO":
			profile.Distro = value
		case "SHELL":
			profile.Shell = value
		case "USER":
			profile.User = value
		case "HOME":
			profile.HomeDir = value
		case "CWD":
			profile.WorkingDir = value
		case "PATH":
			profile.PathPreview = summarizePath(value)
		case "INIT":
			profile.InitSystem = value
		case "CAPABILITIES":
			if value == "" {
				continue
			}
			profile.Capabilities = strings.Split(value, ",")
		}
	}
	profile.Raw = strings.TrimSpace(stdout + stderr)
	profile.Summary = buildHostProfileSummary(profile)
	return profile
}

func summarizePath(value string) string {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) == 0 {
		return ""
	}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		filtered = append(filtered, part)
	}
	if len(filtered) == 0 {
		return ""
	}
	preview := filtered
	if len(preview) > 6 {
		preview = preview[:6]
	}
	return strings.Join(preview, ":")
}

func buildHostProfileSummary(profile models.HostProfile) string {
	lines := []string{
		"hostname: " + firstNonEmpty(profile.Hostname, "unknown"),
		"distro: " + firstNonEmpty(profile.Distro, "unknown"),
		"kernel: " + firstNonEmpty(profile.Kernel, "unknown"),
		"shell: " + firstNonEmpty(profile.Shell, "unknown"),
		"user: " + firstNonEmpty(profile.User, "unknown"),
		"home: " + firstNonEmpty(profile.HomeDir, "unknown"),
		"cwd: " + firstNonEmpty(profile.WorkingDir, "unknown"),
		"path_preview: " + firstNonEmpty(profile.PathPreview, "unknown"),
		"init: " + firstNonEmpty(profile.InitSystem, "unknown"),
	}
	if len(profile.Capabilities) > 0 {
		lines = append(lines, "capabilities: "+strings.Join(profile.Capabilities, ", "))
	}
	return strings.Join(lines, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
