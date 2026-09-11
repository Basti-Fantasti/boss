package env

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/basti-fantasti/bossy/pkg/consts"
	"github.com/basti-fantasti/bossy/pkg/msg"
	"github.com/basti-fantasti/bossy/utils/crypto"
)

// Configuration represents the global configuration for Boss.
// This struct implements the ConfigProvider interface for dependency injection.
// See pkg/env/interfaces.go for interface details.
//
// The configuration is loaded once at startup and injected throughout
// the application via the ConfigProvider interface.
type Configuration struct {
	path                string            `json:"-"`
	Key                 string            `json:"id"`
	Auth                map[string]*Auth  `json:"auth"`
	PurgeTime           int               `json:"purge_after"`
	InternalRefreshRate int               `json:"internal_refresh_rate"`
	LastPurge           time.Time         `json:"last_purge_cache"`
	LastInternalUpdate  time.Time         `json:"last_internal_update"`
	DelphiPath          string            `json:"delphi_path,omitempty"`
	ConfigVersion       int64             `json:"config_version"`
	GitShallow          bool              `json:"git_shallow,omitempty"`
	HostProtocols       map[string]string `json:"host_protocols,omitempty"`
	Aliases             map[string]string `json:"aliases,omitempty"`
	// PresetSource is the catalog repository `bossy preset sync` pulls from.
	// Remembered after the first successful sync so later runs need no flag.
	PresetSource string `json:"preset_source,omitempty"`

	Advices struct {
		SetupPath bool `json:"setup_path,omitempty"`
	} `json:"advices"`
}

// Auth represents stored HTTPS basic-auth credentials for a given host.
// SSH credentials are no longer stored — they come from ssh-agent and
// ~/.ssh/config via the system git binary.
type Auth struct {
	User string `json:"user,omitempty"`
	Pass string `json:"pass,omitempty"`
}

// GetUser returns the decrypted username.
func (a *Auth) GetUser() string {
	ret, err := crypto.Decrypt(crypto.MachineKey(), a.User)
	if err != nil {
		msg.Err("❌ Failed to decrypt user.")
		return ""
	}
	return ret
}

// GetPassword returns the decrypted password.
func (a *Auth) GetPassword() string {
	ret, err := crypto.Decrypt(crypto.MachineKey(), a.Pass)
	if err != nil {
		msg.Die("❌ Failed to decrypt pass: %s", err)
		return ""
	}

	return ret
}

// SetUser encrypts and sets the username.
func (a *Auth) SetUser(user string) {
	if encryptedUser, err := crypto.Encrypt(crypto.MachineKey(), user); err != nil {
		msg.Die("❌ Failed to crypt user: %s", err)
	} else {
		a.User = encryptedUser
	}
}

// SetPass encrypts and sets the password.
func (a *Auth) SetPass(pass string) {
	if cPass, err := crypto.Encrypt(crypto.MachineKey(), pass); err != nil {
		msg.Die("❌ Failed to crypt pass: %s", err)
	} else {
		a.Pass = cPass
	}
}

// GetHTTPSCredentials returns stored HTTPS basic-auth for a host, or
// (false) if none. Implements auth.CredentialStore.
func (c *Configuration) GetHTTPSCredentials(host string) (string, string, bool) {
	a, ok := c.Auth[host]
	if !ok || a == nil {
		return "", "", false
	}
	return a.GetUser(), a.GetPassword(), true
}

// SaveConfiguration saves the configuration to disk.
func (c *Configuration) SaveConfiguration() {
	jsonString, err := json.MarshalIndent(c, "", "\t")
	if err != nil {
		msg.Die("❌ Failed to parse config file", err.Error())
	}

	err = os.MkdirAll(c.path, 0755) // #nosec G301 -- Standard permissions for Boss cache directory
	if err != nil {
		msg.Die("❌ Failed to create path", c.path, err.Error())
	}

	configPath := filepath.Join(c.path, consts.BossConfigFile)
	f, err := os.Create(configPath) // #nosec G304 -- Creating Boss configuration file in known location
	if err != nil {
		msg.Die("❌ Failed to create file ", configPath, err.Error())
		return
	}

	defer f.Close()

	_, err = f.Write(jsonString)
	if err != nil {
		msg.Die("❌ Failed to write cache file", err.Error())
	}
}

// makeDefault creates a default configuration.
func makeDefault(configPath string) *Configuration {
	return &Configuration{
		path:                configPath,
		PurgeTime:           3,
		InternalRefreshRate: 5,
		LastInternalUpdate:  time.Now(),
		Auth:                make(map[string]*Auth),
		Key:                 crypto.Md5MachineID(),
		GitShallow:          false, // Default to full clone for compatibility
	}
}

// LoadConfiguration loads the configuration from disk.
func LoadConfiguration(cachePath string) (*Configuration, error) {
	configuration := &Configuration{
		PurgeTime: 3,
	}

	configFileName := filepath.Join(cachePath, consts.BossConfigFile)
	buffer, err := os.ReadFile(configFileName) // #nosec G304 -- Reading Boss configuration file from cache directory
	if err != nil {
		return makeDefault(cachePath), err
	}
	err = json.Unmarshal(buffer, configuration)
	if err != nil {
		msg.Err("❌ Failed to load cfg %s", err)
		return makeDefault(cachePath), err
	}
	if configuration.Key != crypto.Md5MachineID() {
		msg.Err("❌ Failed to load auth... recreate login accounts")
		configuration.Key = crypto.Md5MachineID()
		configuration.Auth = make(map[string]*Auth)
	}

	configuration.path = cachePath

	return configuration, nil
}
