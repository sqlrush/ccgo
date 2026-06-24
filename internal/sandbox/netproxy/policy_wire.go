package netproxy

// NeedsProxy reports whether p requires a local domain-filtering proxy.
// Returns true when AllowedDomains or DeniedDomains is non-empty — i.e. the
// policy has per-domain intent that cannot be expressed at the kernel level and
// must be enforced via the proxy layer.
//
// This is the gate that callers check before calling StartForSandbox.
func NeedsProxy(allowedDomains, deniedDomains []string) bool {
	return len(allowedDomains) > 0 || len(deniedDomains) > 0
}

// StartForSandbox starts a FilteringProxy for the given allowed/denied domain
// lists and returns the proxy and the env vars to inject into the sandboxed
// process. The caller MUST call the returned stop function (via defer) to
// release resources after the sandboxed command completes.
//
// Usage pattern:
//
//	fp, envVars, stop, err := netproxy.StartForSandbox(allowed, denied)
//	if err != nil { ... }
//	defer stop()
//	cmd.Env = append(os.Environ(), envVars...)
func StartForSandbox(allowedDomains, deniedDomains []string) (*FilteringProxy, []string, func(), error) {
	fp, err := Start(Policy{
		AllowedDomains: allowedDomains,
		DeniedDomains:  deniedDomains,
	})
	if err != nil {
		return nil, nil, func() {}, err
	}
	envVars := EnvForProxy(fp)
	stop := func() { fp.Close() }
	return fp, envVars, stop, nil
}
