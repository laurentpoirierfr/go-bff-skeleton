// Package config charge la configuration avec Viper, dans cet ordre de
// priorité croissante :
//
//  1. les valeurs par défaut, déclarées dans les étiquettes de Config ;
//  2. le fichier YAML (configs/bff.yaml, par exemple) ;
//  3. les variables d'environnement : la clé en majuscules, les points
//     remplacés par « _ » (proxy.default_timeout → PROXY_DEFAULT_TIMEOUT,
//     routes.users.target → ROUTES_USERS_TARGET).
//
// Les étiquettes de Config sont la seule source de vérité : elles donnent les
// valeurs par défaut, le fichier de référence (configs/reference.yaml) et
// l'affichage de la configuration effective (-print-config).
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Étiquettes des champs :
//
//	mapstructure  la clé dans le fichier YAML
//	default       la valeur par défaut (une liste : valeurs séparées par « , »)
//	example       un exemple, pour les clés sans valeur par défaut (routes)
//	doc           la documentation, reprise dans configs/reference.yaml
//	secret        "true" : jamais affichée en clair

// Config est la configuration du BFF.
type Config struct {
	Service   ServiceConfig          `mapstructure:"service" doc:"Identité du service"`
	HTTP      HTTPConfig             `mapstructure:"http" doc:"Serveur HTTP"`
	Web       WebConfig              `mapstructure:"web" doc:"Application web embarquée (le build de web/)"`
	Proxy     ProxyConfig            `mapstructure:"proxy" doc:"Proxy vers les API (réglages communes à toutes les routes)"`
	Routes    map[string]RouteConfig `mapstructure:"routes" doc:"Table de routage : une entrée par API, nommée (users, orders…)"`
	Log       LogConfig              `mapstructure:"log" doc:"Journal"`
	Telemetry TelemetryConfig        `mapstructure:"telemetry" doc:"Traces OpenTelemetry, envoyées au collecteur"`
	Shutdown  ShutdownConfig         `mapstructure:"shutdown" doc:"Arrêt"`
}

type ServiceConfig struct {
	Name string `mapstructure:"name" default:"bff" doc:"Nom du service dans les journaux et les traces"`
}

type HTTPConfig struct {
	Address string `mapstructure:"address" default:":8080" doc:"Adresse d'écoute"`
}

type WebConfig struct {
	CSP string `mapstructure:"csp" default:"default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'" doc:"Politique de sécurité du contenu (Content-Security-Policy) des pages"`
}

type ProxyConfig struct {
	DefaultTimeout time.Duration `mapstructure:"default_timeout" default:"10s" doc:"Délai d'une requête vers un backend, si la route n'en fixe pas"`
	MaxBodyBytes   int64         `mapstructure:"max_body_bytes" default:"1048576" doc:"Taille maximale du corps d'une requête relayée (1 Mio)"`
}

// RouteConfig décrit une route : les requêtes dont le chemin commence par
// Prefix sont relayées vers Target, le reste du chemin et la requête
// (?query) conservés.
type RouteConfig struct {
	Prefix  string        `mapstructure:"prefix" example:"/api/users" doc:"Préfixe des chemins relayés (sous /api/)"`
	Target  string        `mapstructure:"target" example:"http://localhost:8091/api/v1/users" doc:"URL du backend qui remplace le préfixe"`
	Timeout time.Duration `mapstructure:"timeout" example:"5s" doc:"Délai de cette route (facultatif : proxy.default_timeout)"`
}

type LogConfig struct {
	Level  string `mapstructure:"level" default:"info" doc:"Niveau : debug, info, warn ou error"`
	Format string `mapstructure:"format" default:"json" doc:"Format : json (production) ou text (développement)"`
}

type TelemetryConfig struct {
	Enabled     bool    `mapstructure:"enabled" default:"false" doc:"Envoyer les traces au collecteur OpenTelemetry"`
	Endpoint    string  `mapstructure:"endpoint" default:"http://localhost:4318" doc:"URL du collecteur, en OTLP HTTP"`
	SampleRatio float64 `mapstructure:"sample_ratio" default:"1" doc:"Part des nouvelles traces gardées, de 0 à 1 (une trace commencée ailleurs garde sa décision)"`
}

type ShutdownConfig struct {
	Timeout time.Duration `mapstructure:"timeout" default:"15s" doc:"Temps laissé aux traitements en cours, à l'arrêt"`
}

// Options dit où trouver la configuration.
type Options struct {
	// File est le chemin du fichier YAML ; vide, aucun fichier n'est lu.
	File string
	// FileRequired rend le fichier obligatoire : vrai quand l'utilisateur l'a
	// désigné (-config, CONFIG_FILE), faux pour le chemin par défaut, pour
	// qu'un conteneur puisse tourner avec ses seules variables d'environnement.
	FileRequired bool
}

// Load charge et valide la configuration.
func Load(opts Options) (Config, error) {
	_, c, err := load(opts)
	if err != nil {
		return Config{}, err
	}
	if err := c.validate(); err != nil {
		return Config{}, err
	}
	return c, nil
}

func load(opts Options) (*viper.Viper, Config, error) {
	v := viper.New()
	for _, f := range Fields() {
		v.SetDefault(f.Key, f.defaultValue())
	}
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	// Toute clé connue (déclarée, ou présente dans le fichier, comme une
	// route) est surchargée par sa variable d'environnement.
	v.AutomaticEnv()
	// Une route peut aussi être déclarée par l'environnement seul
	// (ROUTES_USERS_PREFIX, ROUTES_USERS_TARGET…) : ces variables sont
	// déclarées à Viper, qui ne connaît sinon que les clés du fichier.
	for _, kv := range os.Environ() {
		if m := routeEnvRe.FindStringSubmatch(strings.SplitN(kv, "=", 2)[0]); m != nil {
			_ = v.BindEnv("routes."+strings.ToLower(m[1])+"."+strings.ToLower(m[2]), m[0])
		}
	}

	if opts.File != "" {
		v.SetConfigFile(opts.File)
		if err := v.ReadInConfig(); err != nil {
			var missing *fs.PathError
			if !errors.As(err, &missing) || opts.FileRequired {
				return nil, Config{}, fmt.Errorf("configuration file %s: %w", opts.File, err)
			}
		}
	}
	var c Config
	if err := v.Unmarshal(&c); err != nil {
		return nil, Config{}, fmt.Errorf("configuration: %w", err)
	}
	for name, r := range c.Routes {
		if r.Timeout == 0 {
			r.Timeout = c.Proxy.DefaultTimeout
			c.Routes[name] = r
		}
	}
	return v, c, nil
}

func (c Config) validate() error {
	var errs []error
	if c.Service.Name == "" {
		errs = append(errs, errors.New("service.name: required"))
	}
	if c.HTTP.Address == "" {
		errs = append(errs, errors.New("http.address: required"))
	}
	if c.Proxy.DefaultTimeout <= 0 {
		errs = append(errs, fmt.Errorf("proxy.default_timeout: %v, must be positive", c.Proxy.DefaultTimeout))
	}
	if c.Proxy.MaxBodyBytes < 1 {
		errs = append(errs, fmt.Errorf("proxy.max_body_bytes: %d, must be positive", c.Proxy.MaxBodyBytes))
	}
	errs = append(errs, c.validateRoutes()...)
	var level slog.Level
	if err := level.UnmarshalText([]byte(c.Log.Level)); err != nil {
		errs = append(errs, fmt.Errorf("log.level: %w", err))
	}
	if c.Log.Format != "json" && c.Log.Format != "text" {
		errs = append(errs, fmt.Errorf("log.format: %q, expected json or text", c.Log.Format))
	}
	if c.Telemetry.Enabled {
		if err := checkURL(c.Telemetry.Endpoint, "http", "https"); err != nil {
			errs = append(errs, fmt.Errorf("telemetry.endpoint: %w", err))
		}
	}
	if c.Telemetry.SampleRatio < 0 || c.Telemetry.SampleRatio > 1 {
		errs = append(errs, fmt.Errorf("telemetry.sample_ratio: %v, expected 0 to 1", c.Telemetry.SampleRatio))
	}
	if c.Shutdown.Timeout <= 0 {
		errs = append(errs, fmt.Errorf("shutdown.timeout: %v, must be positive", c.Shutdown.Timeout))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("invalid configuration:\n%w", err)
	}
	return nil
}

// validateRoutes vérifie chaque route : un préfixe sous /api/, une URL de
// backend absolue, et deux routes qui ne se recouvrent pas.
func (c Config) validateRoutes() []error {
	var errs []error
	prefixes := map[string]string{}
	for _, name := range RouteNames(c.Routes) {
		r := c.Routes[name]
		key := "routes." + name
		if !routeNameRe.MatchString(name) {
			errs = append(errs, fmt.Errorf("%s: route name %q, expected lowercase letters and digits", key, name))
		}
		switch {
		case !strings.HasPrefix(r.Prefix, "/api/") || strings.HasSuffix(r.Prefix, "/") || strings.ContainsAny(r.Prefix, "?#{} "):
			errs = append(errs, fmt.Errorf("%s.prefix: %q, expected a path under /api/ without trailing slash", key, r.Prefix))
		case prefixes[r.Prefix] != "":
			errs = append(errs, fmt.Errorf("%s.prefix: %q, already used by routes.%s", key, r.Prefix, prefixes[r.Prefix]))
		default:
			prefixes[r.Prefix] = name
		}
		if err := checkURL(r.Target, "http", "https"); err != nil {
			errs = append(errs, fmt.Errorf("%s.target: %w", key, err))
		} else if u, _ := url.Parse(r.Target); u.Host == "" || u.RawQuery != "" {
			errs = append(errs, fmt.Errorf("%s.target: %q, expected scheme://host[:port][/path], without query", key, redact(r.Target)))
		}
		if r.Timeout <= 0 {
			errs = append(errs, fmt.Errorf("%s.timeout: %v, must be positive", key, r.Timeout))
		}
	}
	return errs
}

// routeEnvRe reconnaît une variable de route : ROUTES_<NOM>_<CLÉ>.
var routeEnvRe = regexp.MustCompile(`^ROUTES_([A-Z][A-Z0-9]*)_(PREFIX|TARGET|TIMEOUT)$`)

// routeNameRe : des minuscules et des chiffres, sans « _ » ni « - », pour que
// le nom se retrouve sans ambiguïté dans ROUTES_<NOM>_<CLÉ>.
var routeNameRe = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// RouteNames renvoie les noms des routes, triés : un ordre stable pour les
// journaux et les fichiers générés.
func RouteNames(routes map[string]RouteConfig) []string {
	names := make([]string, 0, len(routes))
	for n := range routes {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// SlogLevel renvoie le niveau de journal (déjà validé par Load).
func (l LogConfig) SlogLevel() slog.Level {
	var level slog.Level
	_ = level.UnmarshalText([]byte(l.Level))
	return level
}

func checkURL(raw string, schemes ...string) error {
	if raw == "" {
		return errors.New("required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return errors.New("unparsable URL") // l'erreur d'origine citerait l'URL, mot de passe compris
	}
	if slices.Contains(schemes, u.Scheme) {
		return nil
	}
	return fmt.Errorf("scheme %q, expected %s", u.Scheme, strings.Join(schemes, " or "))
}

// redact masque le mot de passe d'une URL, ou toute autre valeur secrète.
func redact(value string) string {
	if value == "" {
		return ""
	}
	if u, err := url.Parse(value); err == nil && u.Scheme != "" {
		return u.Redacted()
	}
	return "xxxxx"
}

// Field décrit une clé de configuration, d'après les étiquettes de Config.
type Field struct {
	Key     string // proxy.default_timeout
	Env     string // PROXY_DEFAULT_TIMEOUT
	Default string // telle qu'écrite dans l'étiquette
	Example string
	Doc     string
	Secret  bool
	kind    reflect.Type
}

// Env donne le nom de la variable d'environnement d'une clé.
func Env(key string) string { return strings.ToUpper(strings.ReplaceAll(key, ".", "_")) }

// Fields liste les clés à valeur par défaut, dans l'ordre de Config. Les
// routes, nommées par l'utilisateur, n'en font pas partie : voir RouteFields.
func Fields() []Field {
	var fields []Field
	walk(reflect.TypeOf(Config{}), "", func(prefix string, f reflect.StructField) {
		fields = append(fields, field(prefix, f))
	}, nil)
	return fields
}

// RouteFields décrit les clés d'une route nommée name.
func RouteFields(name string) []Field {
	var fields []Field
	walk(reflect.TypeOf(RouteConfig{}), "routes."+name+".", func(prefix string, f reflect.StructField) {
		fields = append(fields, field(prefix, f))
	}, nil)
	return fields
}

func field(prefix string, f reflect.StructField) Field {
	key := prefix + f.Tag.Get("mapstructure")
	return Field{
		Key: key, Env: Env(key), Default: f.Tag.Get("default"), Example: f.Tag.Get("example"),
		Doc: f.Tag.Get("doc"), Secret: f.Tag.Get("secret") == "true", kind: f.Type,
	}
}

// walk parcourt les champs d'une structure ; une map (les routes) est un
// cas à part, traité par l'appelant.
func walk(t reflect.Type, prefix string, leaf func(prefix string, f reflect.StructField), section func(key, doc string)) {
	for i := range t.NumField() {
		f := t.Field(i)
		switch f.Type.Kind() {
		case reflect.Map:
			continue
		case reflect.Struct:
			key := prefix + f.Tag.Get("mapstructure")
			if section != nil {
				section(key, f.Tag.Get("doc"))
			}
			walk(f.Type, key+".", leaf, section)
		default:
			leaf(prefix, f)
		}
	}
}

// defaultValue convertit l'étiquette default pour Viper : une liste pour un
// champ de type slice, du texte sinon (Viper convertit ensuite).
func (f Field) defaultValue() any {
	if f.kind.Kind() == reflect.Slice {
		if f.Default == "" {
			return []string{}
		}
		return strings.Split(f.Default, ",")
	}
	return f.Default
}

// yamlValue écrit une valeur pour un fichier YAML.
func (f Field) yamlValue(value string) string {
	switch {
	case f.kind.Kind() == reflect.Slice:
		if value == "" {
			return "[]"
		}
		return "[" + strings.ReplaceAll(value, ",", ", ") + "]"
	case f.kind.Kind() == reflect.String:
		return strconv.Quote(value)
	case value == "":
		return `""`
	}
	return value
}

func indentOf(key string) string { return strings.Repeat("  ", strings.Count(key, ".")) }
func lastPart(key string) string { return key[strings.LastIndex(key, ".")+1:] }

// Reference renvoie le fichier de référence : toutes les clés, avec leur
// documentation, leur variable d'environnement et leur valeur par défaut, et
// une route d'exemple.
func Reference() string {
	var b strings.Builder
	b.WriteString(`# Référence de la configuration, générée à partir du code
# (internal/platform/config) : ne pas modifier à la main, lancer
# « make config-reference ».
#
# Ordre de priorité : valeurs par défaut (ci-dessous), puis le fichier YAML
# (configs/bff.yaml, ou -config), puis les variables d'environnement. Chaque
# clé a sa variable : la clé en majuscules, les points remplacés par « _ ».
#
# Pour voir la configuration effective, et d'où vient chaque valeur :
#   go run ./cmd/bff -print-config
`)
	fields := Fields()
	i := 0
	writeLeaf := func(f Field, value string) {
		in := indentOf(f.Key)
		fmt.Fprintf(&b, "%s# %s\n%s# env : %s\n", in, f.Doc, in, f.Env)
		if f.Secret {
			fmt.Fprintf(&b, "%s# secret : jamais affiché en clair\n", in)
		}
		fmt.Fprintf(&b, "%s%s: %s\n", in, lastPart(f.Key), f.yamlValue(value))
	}
	walkConfig(func(string, reflect.StructField) {
		writeLeaf(fields[i], fields[i].Default)
		i++
	}, func(key, doc string) {
		in := indentOf(key)
		if in == "" {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "%s# %s\n%s%s:\n", in, doc, in, lastPart(key))
	}, func(doc string) {
		fmt.Fprintf(&b, "\n# %s\n# Pas de valeur par défaut : la route ci-dessous est un exemple.\nroutes:\n  # le nom de la route (env : ROUTES_<NOM>_<CLÉ>)\n  users:\n", doc)
		for _, f := range RouteFields("users") {
			writeLeaf(f, f.Example)
		}
	})
	return b.String()
}

// walkConfig parcourt Config dans l'ordre, en signalant la section des
// routes à sa place.
func walkConfig(leaf func(prefix string, f reflect.StructField), section func(key, doc string), routes func(doc string)) {
	t := reflect.TypeOf(Config{})
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Type.Kind() == reflect.Map {
			routes(f.Tag.Get("doc"))
			continue
		}
		key := f.Tag.Get("mapstructure")
		section(key, f.Tag.Get("doc"))
		walk(f.Type, key+".", leaf, section)
	}
}

// Explain renvoie la configuration effective, chaque valeur suivie de sa
// source (défaut, fichier ou variable d'environnement), les secrets masqués.
// L'erreur signale une configuration invalide : le texte est renvoyé quand
// même, pour comprendre pourquoi.
func Explain(opts Options) (string, error) {
	v, c, err := load(opts)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	if opts.File != "" && v.ConfigFileUsed() != "" {
		if _, statErr := os.Stat(opts.File); statErr == nil {
			fmt.Fprintf(&b, "# fichier : %s\n", opts.File)
		}
	}
	writeLeaf := func(f Field, value string) {
		if f.Secret {
			value = redact(value)
		}
		source := "défaut"
		switch {
		case os.Getenv(f.Env) != "":
			source = "env " + f.Env
		case v.InConfig(f.Key):
			source = "fichier"
		}
		fmt.Fprintf(&b, "%s%s: %s  # %s\n", indentOf(f.Key), lastPart(f.Key), f.yamlValue(value), source)
	}
	fields := Fields()
	i := 0
	walkConfig(func(string, reflect.StructField) {
		f := fields[i]
		i++
		value := v.GetString(f.Key)
		if f.kind.Kind() == reflect.Slice {
			value = strings.Join(v.GetStringSlice(f.Key), ",")
		}
		writeLeaf(f, value)
	}, func(key, _ string) {
		fmt.Fprintf(&b, "%s%s:\n", indentOf(key), lastPart(key))
	}, func(string) {
		b.WriteString("routes:\n")
		for _, name := range RouteNames(c.Routes) {
			fmt.Fprintf(&b, "  %s:\n", name)
			r := c.Routes[name]
			values := map[string]string{"prefix": r.Prefix, "target": r.Target, "timeout": r.Timeout.String()}
			for _, f := range RouteFields(name) {
				writeLeaf(f, values[lastPart(f.Key)])
			}
		}
	})
	return b.String(), c.validate()
}

// EnvExample renvoie un fichier .env d'exemple : toutes les clés, routes
// comprises, avec leur documentation et leur valeur effective pour ces
// options (en pratique, les valeurs de développement du fichier YAML). Le
// format KEY=value, sans guillemets, convient au shell (set -a; . fichier),
// à VS Code (envFile) et à docker run --env-file.
//
// Les variables des routes ne surchargent que des routes déclarées dans le
// fichier YAML : elles servent à changer une cible selon l'environnement.
func EnvExample(opts Options, title string) (string, error) {
	v, c, err := load(opts)
	if err != nil {
		return "", err
	}
	base := filepath.Base(opts.File)
	envFile := "configs/" + strings.TrimSuffix(base, filepath.Ext(base)) + ".env.example"
	var b strings.Builder
	fmt.Fprintf(&b, `# %s
#
# Généré à partir du code (internal/platform/config) : ne pas modifier à la
# main, lancer « make config-reference ». Les valeurs sont celles du
# développement local ; la documentation complète des clés est dans
# configs/reference.yaml.
#
# Les variables ROUTES_* surchargent des routes déclarées dans le fichier
# YAML (par exemple la cible d'une API selon l'environnement).
#
# Utilisation :
#   set -a; . ./%s; set +a     (shell, avant go run)
#   docker run --env-file %s …
#   VS Code : "envFile" dans launch.json
`, title, envFile, envFile)
	section := ""
	write := func(f Field, value string) {
		top, _, _ := strings.Cut(f.Key, ".")
		if top != section {
			section = top
			fmt.Fprintf(&b, "\n# --- %s\n", top)
		}
		if strings.ContainsAny(value, " ;'\"$`\\") {
			// Une valeur avec espaces, « ; » ou guillemets ne s'écrit pas de
			// façon sûre à la fois pour le shell (qui l'interpréterait) et pour
			// docker --env-file (qui garde les guillemets) : elle est laissée
			// en commentaire, la valeur effective restant celle du code ou du
			// fichier YAML.
			fmt.Fprintf(&b, "\n# %s\n# (valeur à espaces : à définir hors de ce fichier si besoin)\n# %s=%s\n", f.Doc, f.Env, value)
			return
		}
		fmt.Fprintf(&b, "\n# %s\n%s=%s\n", f.Doc, f.Env, value)
	}
	for _, f := range Fields() {
		value := v.GetString(f.Key)
		if f.kind.Kind() == reflect.Slice {
			value = strings.Join(v.GetStringSlice(f.Key), ",")
		}
		write(f, value)
	}
	for _, name := range RouteNames(c.Routes) {
		r := c.Routes[name]
		values := map[string]string{"prefix": r.Prefix, "target": r.Target, "timeout": r.Timeout.String()}
		for _, f := range RouteFields(name) {
			write(f, values[lastPart(f.Key)])
		}
	}
	return b.String(), nil
}
