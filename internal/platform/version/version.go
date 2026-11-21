// Package version donne la version du programme, fixée à la compilation :
//
//	go build -ldflags "-X github.com/laurentpoirierfr/go-bff-skeleton/internal/platform/version.Version=1.2.0"
package version

// Version vaut « dev » hors d'une compilation de livraison.
var Version = "dev"
