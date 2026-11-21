package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestMain isole les tests de l'environnement du développeur.
func TestMain(m *testing.M) {
	for _, f := range Fields() {
		os.Unsetenv(f.Env)
	}
	for _, kv := range os.Environ() {
		if k, _, _ := strings.Cut(kv, "="); strings.HasPrefix(k, "ROUTES_") {
			os.Unsetenv(k)
		}
	}
	os.Exit(m.Run())
}

func writeFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

const twoRoutes = `
routes:
  users:
    prefix: /api/users
    target: http://users:8080/api/v1/users
    timeout: 2s
  orders:
    prefix: /api/orders
    target: http://orders:8080/api/v1/orders
`

func TestDefaults(t *testing.T) {
	c, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Service.Name != "bff" || c.HTTP.Address != ":8080" || c.Proxy.DefaultTimeout != 10*time.Second ||
		c.Proxy.MaxBodyBytes != 1<<20 || !strings.Contains(c.Web.CSP, "default-src 'self'") || len(c.Routes) != 0 {
		t.Errorf("%+v", c)
	}
}

// Les routes viennent du fichier ; une route sans délai prend le délai par
// défaut ; l'environnement change une cible.
func TestRoutes(t *testing.T) {
	t.Setenv("ROUTES_USERS_TARGET", "http://users.prod:8080/api/v1/users")
	c, err := Load(Options{File: writeFile(t, twoRoutes), FileRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	if u := c.Routes["users"]; u.Prefix != "/api/users" || u.Target != "http://users.prod:8080/api/v1/users" || u.Timeout != 2*time.Second {
		t.Errorf("users %+v", u)
	}
	if o := c.Routes["orders"]; o.Timeout != 10*time.Second {
		t.Errorf("orders %+v", o)
	}
	if got := RouteNames(c.Routes); !reflect.DeepEqual(got, []string{"orders", "users"}) {
		t.Errorf("names %v", got)
	}
}

// Une route se déclare aussi par l'environnement seul.
func TestRouteFromEnv(t *testing.T) {
	t.Setenv("ROUTES_PRODUCTS_PREFIX", "/api/products")
	t.Setenv("ROUTES_PRODUCTS_TARGET", "http://catalog:8080/v2/products")
	c, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if p := c.Routes["products"]; p.Prefix != "/api/products" || p.Target != "http://catalog:8080/v2/products" || p.Timeout != 10*time.Second {
		t.Errorf("%+v", c.Routes)
	}
}

func TestRouteValidation(t *testing.T) {
	for name, tc := range map[string]struct{ yaml, want string }{
		"préfixe hors /api":  {"routes:\n  users:\n    prefix: /users\n    target: http://u/api\n", "routes.users.prefix"},
		"barre finale":       {"routes:\n  users:\n    prefix: /api/users/\n    target: http://u/api\n", "without trailing slash"},
		"préfixe en double":  {"routes:\n  users:\n    prefix: /api/x\n    target: http://u/a\n  more:\n    prefix: /api/x\n    target: http://u/b\n", "already used"},
		"cible relative":     {"routes:\n  users:\n    prefix: /api/users\n    target: /api/v1/users\n", "routes.users.target"},
		"cible avec requête": {"routes:\n  users:\n    prefix: /api/users\n    target: http://u/api?x=1\n", "without query"},
		"nom invalide":       {"routes:\n  user-svc:\n    prefix: /api/users\n    target: http://u/api\n", "route name"},
		"délai négatif":      {"routes:\n  users:\n    prefix: /api/users\n    target: http://u/api\n    timeout: -1s\n", "routes.users.timeout"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Load(Options{File: writeFile(t, tc.yaml), FileRequired: true})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMissingFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.yaml")
	if _, err := Load(Options{File: missing}); err != nil {
		t.Errorf("optional file: %v", err)
	}
	if _, err := Load(Options{File: missing, FileRequired: true}); err == nil {
		t.Error("a required missing file must be an error")
	}
}

func TestValidationErrors(t *testing.T) {
	for k, v := range map[string]string{
		"LOG_LEVEL": "chatty", "LOG_FORMAT": "xml", "SHUTDOWN_TIMEOUT": "-1s",
		"PROXY_DEFAULT_TIMEOUT": "0s", "PROXY_MAX_BODY_BYTES": "0", "TELEMETRY_SAMPLE_RATIO": "2",
	} {
		t.Setenv(k, v)
	}
	_, err := Load(Options{})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"log.level", "log.format", "shutdown.timeout", "proxy.default_timeout", "proxy.max_body_bytes", "telemetry.sample_ratio"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s missing from:\n%v", want, err)
		}
	}
}

// Chaque clé et chaque section sont documentées ; chaque clé de route a un
// exemple.
func TestEveryFieldDocumented(t *testing.T) {
	walk(reflect.TypeOf(Config{}), "", func(prefix string, f reflect.StructField) {
		if f.Tag.Get("mapstructure") == "" || f.Tag.Get("doc") == "" {
			t.Errorf("%s%s: mapstructure or doc tag missing", prefix, f.Name)
		}
	}, func(key, doc string) {
		if doc == "" {
			t.Errorf("section %s: doc tag missing", key)
		}
	})
	for _, f := range RouteFields("x") {
		if f.Doc == "" || f.Example == "" {
			t.Errorf("%s: doc or example missing", f.Key)
		}
	}
}

const referencePath = "../../../configs/reference.yaml"

// generated vérifie qu'un fichier généré est à jour ; make config-reference
// les régénère tous.
func generated(t *testing.T, path, content string) {
	t.Helper()
	if os.Getenv("UPDATE_CONFIG_REFERENCE") != "" {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != content {
		t.Errorf("%s is out of date: run make config-reference (%v)", filepath.Base(path), err)
	}
}

func TestReferenceUpToDate(t *testing.T) { generated(t, referencePath, Reference()) }

// La référence, lue comme un fichier, redonne les valeurs par défaut, plus la
// route d'exemple, valide.
func TestReferenceIsValid(t *testing.T) {
	_, fromDefaults, err := load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	fromReference, err := Load(Options{File: referencePath, FileRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(fromReference.Routes) != 1 || fromReference.Routes["users"].Target == "" {
		t.Errorf("example route: %+v", fromReference.Routes)
	}
	fromReference.Routes = nil
	if !reflect.DeepEqual(fromDefaults, fromReference) {
		t.Errorf("reference:\n%+v\ndefaults:\n%+v", fromReference, fromDefaults)
	}
}

// Le fichier .env d'exemple est à jour, et suffit seul : chargé comme
// variables d'environnement, sans fichier YAML, il donne la configuration du
// fichier YAML, routes comprises.
func TestEnvExample(t *testing.T) {
	yaml := "../../../configs/bff.yaml"
	content, err := EnvExample(Options{File: yaml, FileRequired: true}, "Variables d'environnement du BFF")
	if err != nil {
		t.Fatal(err)
	}
	generated(t, "../../../configs/bff.env.example", content)
	fromYAML, err := Load(Options{File: yaml, FileRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	for line := range strings.Lines(content) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, _ := strings.Cut(line, "=")
		t.Setenv(k, v)
	}
	fromEnv, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(fromEnv, fromYAML) {
		t.Errorf("from env:\n%+v\nfrom YAML:\n%+v", fromEnv, fromYAML)
	}
}

func TestExplain(t *testing.T) {
	t.Setenv("ROUTES_USERS_TARGET", "http://u:secret@users.prod/api/v1/users")
	out, err := Explain(Options{File: writeFile(t, twoRoutes), FileRequired: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`address: ":8080"  # défaut`,
		`prefix: "/api/orders"  # fichier`,
		`target: "http://u:secret@users.prod/api/v1/users"  # env ROUTES_USERS_TARGET`,
		`timeout: 10s  # défaut`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from:\n%s", want, out)
		}
	}
}

func TestShippedFile(t *testing.T) {
	if _, err := Load(Options{File: "../../../configs/bff.yaml", FileRequired: true}); err != nil {
		t.Error(err)
	}
}
