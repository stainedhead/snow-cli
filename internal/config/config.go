// Package config loads the strict YAML snow config file (FR-002) and
// validates the instance host (spec D-a). Every failure is a usage error
// (exit 2).
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/snow-cli/internal/domain"
)

// Error is a config problem; it maps to exit 2.
type Error struct {
	Msg  string
	Hnt  string
	Wrap error
}

func (e *Error) Error() string {
	if e.Wrap != nil {
		return e.Msg + ": " + e.Wrap.Error()
	}
	return e.Msg
}

// Unwrap returns the wrapped error.
func (e *Error) Unwrap() error { return e.Wrap }

// Category marks the error as a usage problem.
func (*Error) Category() output.Category { return output.CategoryUsage }

// Hint returns the remediation text.
func (e *Error) Hint() string { return e.Hnt }

func errf(format string, a ...any) error { return &Error{Msg: fmt.Sprintf(format, a...)} }

// Config is the parsed config file.
type Config struct {
	DefaultProfile string             `yaml:"default_profile"`
	Profiles       map[string]Profile `yaml:"profiles"`
}

// Profile is one named block.
type Profile struct {
	Mode     string    `yaml:"mode"`
	AgentID  string    `yaml:"agent_id"`
	Instance Instance  `yaml:"instance"`
	Okta     Okta      `yaml:"okta"`
	Daemon   Daemon    `yaml:"daemon"`
	Audit    Audit     `yaml:"audit"`
	Policy   PolicyRef `yaml:"policy"`
	Incident Incident  `yaml:"incident"`
	Whoami   Whoami    `yaml:"whoami"`
	Selftest Selftest  `yaml:"selftest"`
}

// Instance names the ServiceNow instance.
type Instance struct {
	Host    string `yaml:"host"`
	Release string `yaml:"release"`
}

// Okta configures human-mode login.
type Okta struct {
	Issuer    string `yaml:"issuer"`
	ClientID  string `yaml:"client_id"`
	TokenType string `yaml:"token_type"`
}

// Daemon configures the credential daemon client. An empty Socket defers to
// the adapter: AGENT_OKTA_D_SOCKET, then the platform default.
type Daemon struct {
	Socket   string `yaml:"socket"`
	Provider string `yaml:"provider"`
}

// Audit locates the audit log.
type Audit struct {
	Path string `yaml:"path"`
}

// PolicyRef locates the policy file.
type PolicyRef struct {
	Path string `yaml:"path"`
	// AllowOverride lets the --policy flag select a policy other than the
	// one configured for the profile (FR-R06). Default false: in agent mode
	// --policy is refused, and a named policy must match the profile mode.
	AllowOverride bool `yaml:"allow_override"`
}

// Whoami configures the identity endpoint.
// ASSUMPTION(unverified against a real instance): the custom scripted whoami
// endpoint path and response shape (A-02).
type Whoami struct {
	Path string `yaml:"path"`
}

// Selftest names the fixture incidents used by `snow selftest --include-writes`.
type Selftest struct {
	// FixtureIncident is an incident the identity may touch but never resolve.
	FixtureIncident string `yaml:"fixture_incident"`
	// ForeignIncident is an incident the identity must not be able to update.
	ForeignIncident string `yaml:"foreign_incident"`
}

// Incident carries instance-specific incident settings.
type Incident struct {
	States     map[string]string `yaml:"states"`
	Categories []string          `yaml:"categories"`
	Scale      domain.Scale      `yaml:"scale"`
	CreateVia  string            `yaml:"create_via"`
	Producer   string            `yaml:"producer"`
}

// Defaults.
const (
	DefaultProvider   = "snow"
	DefaultWhoamiPath = "/api/x_corp_agent/v1/whoami"
	EnvInstanceHost   = "SNOW_INSTANCE_HOST"
	EnvConfig         = "SNOW_CONFIG"
)

// Resolved is a validated profile with defaults and the effective host.
type Resolved struct {
	Profile
	Name string
	Mode domain.Mode
	Host string
}

// Parse decodes and validates config bytes: unknown and duplicate keys fail.
func Parse(data []byte) (*Config, error) {
	if strings.TrimSpace(string(data)) == "" {
		return nil, errf("config file is empty")
	}
	var c Config
	if err := yaml.UnmarshalWithOptions(data, &c, yaml.Strict()); err != nil {
		return nil, &Error{Msg: "invalid config", Wrap: errors.New(firstLine(err.Error()))}
	}
	if len(c.Profiles) == 0 {
		return nil, errf("config defines no profiles")
	}
	if c.DefaultProfile != "" {
		if _, ok := c.Profiles[c.DefaultProfile]; !ok {
			return nil, errf("default_profile %q is not defined under profiles", c.DefaultProfile)
		}
	}
	for _, name := range sortedNames(c.Profiles) {
		if err := c.Profiles[name].validate(name); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func sortedNames(m map[string]Profile) []string {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func (p Profile) validate(name string) error {
	if _, err := domain.ParseMode(p.Mode); err != nil {
		return errf("profile %q: %v", name, err)
	}
	if p.Instance.Host != "" {
		if _, err := ValidateHost(p.Instance.Host); err != nil {
			return errf("profile %q: instance.host: %v", name, err)
		}
	}
	switch p.Incident.CreateVia {
	case "", "producer", "table":
	default:
		return errf("profile %q: incident.create_via %q: want producer or table", name, p.Incident.CreateVia)
	}
	switch p.Okta.TokenType {
	case "", "access", "id":
	default:
		return errf("profile %q: okta.token_type %q: want access or id", name, p.Okta.TokenType)
	}
	if p.Incident.Scale != (domain.Scale{}) {
		if err := p.Incident.Scale.Validate(); err != nil {
			return errf("profile %q: incident.scale: %v", name, err)
		}
	}
	return nil
}

var (
	hostRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)*(:[0-9]{1,5})?$`)
)

// ValidateHost accepts only a bare DNS name with an optional port and returns
// it lowercased. Schemes, paths, userinfo, wildcards and anything else fail.
func ValidateHost(h string) (string, error) {
	lower := strings.ToLower(h)
	if !hostRe.MatchString(lower) {
		return "", &Error{
			Msg: fmt.Sprintf("invalid instance host %q: want a bare DNS name such as acme.service-now.com (no scheme, path, userinfo or wildcard)", h),
		}
	}
	if _, port, ok := strings.Cut(lower, ":"); ok {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", errf("invalid instance host %q: port out of range", h)
		}
	}
	return lower, nil
}

// Resolve picks a profile (name, else default_profile, else the only one),
// applies defaults, and resolves the host: the configured host wins; the
// SNOW_INSTANCE_HOST variable only fills a missing one (D-a).
func (c *Config) Resolve(name string, env func(string) string) (Resolved, error) {
	if name == "" {
		name = c.DefaultProfile
	}
	if name == "" && len(c.Profiles) == 1 {
		for n := range c.Profiles {
			name = n
		}
	}
	if name == "" {
		return Resolved{}, &Error{Msg: "no profile selected", Hnt: "pass --profile or set default_profile in the config file"}
	}
	p, ok := c.Profiles[name]
	if !ok {
		return Resolved{}, errf("unknown profile %q", name)
	}
	mode, _ := domain.ParseMode(p.Mode)
	host := p.Instance.Host
	if host == "" && env != nil {
		host = env(EnvInstanceHost)
	}
	if host == "" {
		return Resolved{}, &Error{Msg: fmt.Sprintf("profile %q has no instance.host", name), Hnt: "set instance.host in the config file or " + EnvInstanceHost}
	}
	host, err := ValidateHost(host)
	if err != nil {
		return Resolved{}, err
	}
	if p.Incident.CreateVia == "" {
		p.Incident.CreateVia = "producer"
	}
	if p.Incident.Scale == (domain.Scale{}) {
		p.Incident.Scale = domain.DefaultScale()
	}
	if p.Okta.TokenType == "" {
		p.Okta.TokenType = "access" // ASSUMPTION(unverified against a real instance): access token (A-05).
	}
	if p.Daemon.Provider == "" {
		p.Daemon.Provider = DefaultProvider
	}
	if p.Whoami.Path == "" {
		p.Whoami.Path = DefaultWhoamiPath
	}
	return Resolved{Profile: p, Name: name, Mode: mode, Host: host}, nil
}

// Load reads and parses a config file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &Error{Msg: "cannot read config file " + path, Wrap: errors.Unwrap(err), Hnt: "create the file or pass --config"}
	}
	return Parse(data)
}

// ResolvePath returns the config path: the flag, else SNOW_CONFIG, else
// <home>/.config/snow/config.yaml.
func ResolvePath(flagValue string, env func(string) string, home string) string {
	if flagValue != "" {
		return flagValue
	}
	if v := env(EnvConfig); v != "" {
		return v
	}
	return filepath.Join(home, ".config", "snow", "config.yaml")
}
