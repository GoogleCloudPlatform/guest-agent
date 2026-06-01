package agentcrypto

const (
	// defaultCredsDir is the directory location for MTLS MDS credentials.
	defaultCredsDir = "/var/run/google-mds-mtls"
)

var (
	// certUpdaters is a map of known CA certificate updaters with the local directory paths for certificates.
	certUpdaters = map[string][]string{
		"certctl": {"/usr/local/share/certs"},
	}
)
