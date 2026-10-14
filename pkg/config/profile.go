package config

// Profile is the active config profile, provided in DI by the config module.
type Profile string

// IsProduction reports whether the profile must be treated as production:
// anything other than "dev" or "test", including an unset profile.
func (p Profile) IsProduction() bool {
	return p != "dev" && p != "test"
}
