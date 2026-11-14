// Package version хранит версию сборки сервера.
package version

// commit подставляется при сборке: -ldflags "-X .../internal/version.commit=<хэш>".
var commit = ""

// String возвращает короткий хэш коммита сборки или "dev", если сборка локальная.
func String() string {
	if commit == "" {
		return "dev"
	}
	return commit
}
