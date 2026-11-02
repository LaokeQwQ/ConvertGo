package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func testManifest(t *testing.T, priv ed25519.PrivateKey, url string, data []byte) []byte {
	t.Helper()
	sum := sha256.Sum256(data)
	m := Manifest{Version: "v1.2.3", Assets: []Asset{{OS: runtime.GOOS, Arch: runtime.GOARCH, Filename: "package", URL: url, SHA256: fmt.Sprintf("%x", sum[:]), Size: int64(len(data))}}}
	sig, err := signManifest(m, priv)
	if err != nil {
		t.Fatal(err)
	}
	m.Signature = base64.StdEncoding.EncodeToString(sig)
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func testServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	s := httptest.NewTLSServer(handler)
	t.Cleanup(s.Close)
	return s
}

func sourceForTest(url string, pub ed25519.PublicKey, server *httptest.Server) UpdateSource {
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	return UpdateSource{Kind: SourceMirror, MirrorURL: url, publicKey: pub, rootCAs: pool}
}

func TestCheckAndDownloadValidSignedManifest(t *testing.T) {
	pub, priv := testKeys(t)
	payload := []byte("valid package")
	var server *httptest.Server
	server = testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			_, _ = w.Write(testManifest(t, priv, server.URL+"/package", payload))
		case "/package":
			_, _ = w.Write(payload)
		default:
			http.NotFound(w, r)
		}
	}))
	source := sourceForTest(server.URL+"/manifest.json", pub, server)
	release, err := Check(context.Background(), "owner/repo", source)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "download.pkg")
	if err := Download(context.Background(), release, RuntimeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("download = %q, want %q", got, payload)
	}
}

func TestCheckRejectsManifestTampering(t *testing.T) {
	pub, priv := testKeys(t)
	server := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		b := testManifest(t, priv, "https://github.com/owner/repo/releases/download/v1/package", []byte("x"))
		b = []byte(strings.Replace(string(b), `"version":"v1.2.3"`, `"version":"v9.9.9"`, 1))
		_, _ = w.Write(b)
	}))
	_, err := Check(context.Background(), "owner/repo", sourceForTest(server.URL+"/manifest.json", pub, server))
	if err == nil {
		t.Fatal("tampered manifest was accepted")
	}
}

func TestDownloadRejectsWrongTarget(t *testing.T) {
	pub, priv := testKeys(t)
	payload := []byte("package")
	var server *httptest.Server
	server = testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json" {
			_, _ = w.Write(testManifest(t, priv, server.URL+"/package", payload))
			return
		}
		_, _ = w.Write(payload)
	}))
	release, err := Check(context.Background(), "owner/repo", sourceForTest(server.URL+"/manifest.json", pub, server))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "download.pkg")
	if err := Download(context.Background(), release, RuntimeTarget{OS: "other", Arch: runtime.GOARCH}, dst); err == nil {
		t.Fatal("wrong target was accepted")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("destination exists after rejection: %v", err)
	}
}

func TestCheckRejectsDisallowedRedirect(t *testing.T) {
	pub, _ := testKeys(t)
	outside := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	server := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", outside.URL)
		w.WriteHeader(http.StatusFound)
	}))
	_, err := Check(context.Background(), "owner/repo", sourceForTest(server.URL+"/manifest.json", pub, server))
	if err == nil {
		t.Fatal("disallowed redirect was followed")
	}
}

func TestMirrorSourceAllowsOnlyItsOriginAndGitHubReleaseHosts(t *testing.T) {
	source := UpdateSource{Kind: SourceMirror, MirrorURL: "https://mirror.example/manifest.json"}
	client, err := newHTTPClient(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"https://mirror.example/package", "https://github.com/owner/repo/releases/download/v1.2.3/app.zip", "https://release-assets.githubusercontent.com/package"} {
		req, err := http.NewRequest(http.MethodGet, raw, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := client.CheckRedirect(req, nil); err != nil {
			t.Errorf("approved update host %q rejected: %v", raw, err)
		}
		asset := Asset{OS: "darwin", Arch: "arm64", Filename: "app.zip", URL: raw, SHA256: strings.Repeat("0", 64), Size: 1}
		if err := validateAsset(asset, source); err != nil {
			t.Errorf("approved asset host %q rejected: %v", raw, err)
		}
	}
	req, err := http.NewRequest(http.MethodGet, "https://attacker.example/package", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.CheckRedirect(req, nil); err == nil {
		t.Fatal("redirect to an unrelated origin was accepted")
	}
	asset := Asset{OS: "darwin", Arch: "arm64", Filename: "app.zip", URL: req.URL.String(), SHA256: strings.Repeat("0", 64), Size: 1}
	if err := validateAsset(asset, source); err == nil {
		t.Fatal("asset on an unrelated origin was accepted")
	}
}

func TestManifestRejectsPathUnsafeVersion(t *testing.T) {
	encoded, err := json.Marshal(Manifest{Version: "../v1.2.3", Assets: []Asset{{Filename: "app.zip"}}, Signature: "signature"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeManifest(encoded); err == nil {
		t.Fatal("path-unsafe version was accepted")
	}
}

func TestCheckAndDownloadEnforceSizeLimits(t *testing.T) {
	pub, priv := testKeys(t)
	payload := []byte("package")
	var server *httptest.Server
	server = testServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json" {
			_, _ = w.Write(testManifest(t, priv, server.URL+"/package", payload))
			return
		}
		_, _ = w.Write(payload)
	}))
	release, err := Check(context.Background(), "owner/repo", sourceForTest(server.URL+"/manifest.json", pub, server))
	if err != nil {
		t.Fatal(err)
	}
	release.Assets[0].Size = maxAssetSize + 1
	manifest := Manifest{Version: release.Version, Assets: release.Assets}
	sig, err := signManifest(manifest, priv)
	if err != nil {
		t.Fatal(err)
	}
	release.Signature = base64.StdEncoding.EncodeToString(sig)
	if err := Download(context.Background(), release, RuntimeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH}, filepath.Join(t.TempDir(), "out")); err == nil {
		t.Fatal("oversize asset metadata was accepted")
	}
}

func TestEmptyMirrorResetsToGitHubSource(t *testing.T) {
	source := UpdateSource{Kind: SourceMirror, MirrorURL: "https://mirror.example/manifest.json"}
	source.Reset()
	if source.MirrorURL != "" {
		t.Fatalf("mirror remains configured: %q", source.MirrorURL)
	}
	if !source.usesGitHub() {
		t.Fatal("empty mirror did not select canonical GitHub")
	}
}

func TestCheckRejectsOversizedManifest(t *testing.T) {
	pub, _ := testKeys(t)
	server := testServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, maxManifestSize+1))
	}))
	if _, err := Check(context.Background(), "owner/repo", sourceForTest(server.URL+"/manifest.json", pub, server)); err == nil {
		t.Fatal("oversized manifest was accepted")
	}
}

func TestManifestSignatureEncodingIsDeterministic(t *testing.T) {
	pub, priv := testKeys(t)
	m := Manifest{Version: "v1.0.0", Assets: []Asset{{OS: "z", Arch: "a", Filename: "z", URL: "https://github.com/a", SHA256: strings.Repeat("0", 64), Size: 1}, {OS: "a", Arch: "z", Filename: "a", URL: "https://github.com/b", SHA256: strings.Repeat("1", 64), Size: 1}}}
	sig, err := signManifest(m, priv)
	if err != nil {
		t.Fatal(err)
	}
	m.Signature = base64.StdEncoding.EncodeToString(sig)
	encoded, err := marshalManifest(m, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyManifest(m, pub); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "signature") {
		t.Fatal("signature included in signed encoding")
	}
	if strings.HasSuffix(string(encoded), "\n") {
		t.Fatal("canonical signed payload has a trailing newline")
	}
	if strings.Index(string(encoded), `"os":"a"`) > strings.Index(string(encoded), `"os":"z"`) {
		t.Fatal("assets were not sorted before signing")
	}
}
