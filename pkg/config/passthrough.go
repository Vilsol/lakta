package config

// Passthrough captures arbitrary config keys (via koanf's ",remain") and carries
// the target struct type T for documentation generators to discover via reflect.
type Passthrough[T any] map[string]any
