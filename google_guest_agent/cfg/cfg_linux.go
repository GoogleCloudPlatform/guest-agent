package cfg

const (
	// InstallPathPrefix is / on Linux because the Guest Agent and related
	// files are installed directly relative to root like /usr and /etc.
	InstallPathPrefix = "/"

	// DataPathPrefix is the path prefix for persistent application data.
	DataPathPrefix = "/var/lib"
)
