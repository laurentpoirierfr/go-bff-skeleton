// Package frontend embarque le build de l'application React (dist/, produit
// par npm run build) dans le binaire du BFF : un seul binaire à déployer, sans
// serveur web à côté.
//
// Ce fichier est dans frontend/ parce que go:embed ne remonte pas dans les
// répertoires parents. dist/.gitkeep garantit que la directive compile même
// avant le premier build (le BFF sert alors une page qui l'indique).
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Dist renvoie le build, avec dist/ pour racine.
func Dist() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err) // impossible : dist est embarqué
	}
	return sub
}
