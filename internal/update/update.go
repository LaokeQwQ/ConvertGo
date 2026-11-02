package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

var releaseVersionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

const (
	maxManifestSize = 1 << 20
	maxAssetSize    = 512 << 20
	manifestName    = "update-manifest.json"
)

const productionPublicKeyPEM = `-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEAbG1swJYV/cc66ecIg+mAb6FP3D+eSSdHnvknMm3uBU0=
-----END PUBLIC KEY-----`

type SourceKind string

const (
	SourceGitHub SourceKind = "github"
	SourceMirror SourceKind = "mirror"
)

// UpdateSource selects canonical GitHub or one explicit HTTPS mirror. An empty
// source kind is treated as GitHub for compatibility with zero-value settings.
type UpdateSource struct {
	Kind      SourceKind `json:"kind"`
	MirrorURL string     `json:"mirrorUrl,omitempty"`

	publicKey ed25519.PublicKey
	rootCAs   *x509.CertPool
}

// Reset clears mirror selection and restores the canonical GitHub source.
func (s *UpdateSource) Reset() {
	s.Kind = SourceGitHub
	s.MirrorURL = ""
}

func (s UpdateSource) usesGitHub() bool { return s.Kind == "" || s.Kind == SourceGitHub }

type Asset struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

type Manifest struct {
	Version   string  `json:"version"`
	Assets    []Asset `json:"assets"`
	Signature string  `json:"signature"`
}

// Release contains the signed manifest data returned by Check. Signature is
// retained so Download can verify the metadata again before using it.
type Release struct {
	Version   string  `json:"version"`
	Assets    []Asset `json:"assets"`
	Signature string  `json:"signature"`

	publicKey ed25519.PublicKey
	source    UpdateSource
}

type RuntimeTarget struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

func CurrentTarget() RuntimeTarget { return RuntimeTarget{OS: runtime.GOOS, Arch: runtime.GOARCH} }

// Check retrieves and verifies the latest signed manifest from GitHub Releases
// or the single explicitly selected HTTPS mirror.
func Check(ctx context.Context, repository string, source UpdateSource) (Release, error) {
	client, err := newHTTPClient(source)
	if err != nil {
		return Release{}, err
	}
	defer client.CloseIdleConnections()
	var manifestURL string
	if source.usesGitHub() {
		manifestURL, err = githubManifestURL(ctx, client, repository)
		if err != nil {
			return Release{}, err
		}
	} else {
		manifestURL = source.MirrorURL
	}
	manifestLocation, err := parseHTTPSURL(manifestURL)
	if err != nil {
		return Release{}, fmt.Errorf("invalid update manifest URL: %w", err)
	}
	if !sourceAllowsURL(manifestLocation, source) {
		return Release{}, errors.New("update manifest URL is outside the selected source")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return Release{}, fmt.Errorf("create update manifest request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, fmt.Errorf("fetch update manifest: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Release{}, fmt.Errorf("fetch update manifest: unexpected HTTP status %s", resp.Status)
	}
	body, err := readBounded(resp.Body, maxManifestSize)
	if err != nil {
		return Release{}, fmt.Errorf("read update manifest: %w", err)
	}
	manifest, err := decodeManifest(body)
	if err != nil {
		return Release{}, err
	}
	pub := source.publicKey
	if len(pub) == 0 {
		pub, err = productionPublicKey()
		if err != nil {
			return Release{}, err
		}
	}
	if err := verifyManifest(manifest, pub); err != nil {
		return Release{}, err
	}
	for _, asset := range manifest.Assets {
		if err := validateAsset(asset, source); err != nil {
			return Release{}, err
		}
	}
	return Release{Version: manifest.Version, Assets: manifest.Assets, Signature: manifest.Signature, publicKey: append(ed25519.PublicKey(nil), pub...), source: source}, nil
}

// Download verifies the signed release, selects the exact runtime target, then
// downloads and verifies the package into a temporary file before publishing it.
func Download(ctx context.Context, release Release, target RuntimeTarget, dst string) (retErr error) {
	if dst == "" {
		return errors.New("download destination is empty")
	}
	if target.OS == "" || target.Arch == "" {
		return errors.New("runtime target is incomplete")
	}
	manifest := Manifest{Version: release.Version, Assets: release.Assets, Signature: release.Signature}
	pub := release.publicKey
	if len(pub) == 0 {
		var err error
		pub, err = productionPublicKey()
		if err != nil {
			return err
		}
	}
	if err := verifyManifest(manifest, pub); err != nil {
		return err
	}
	var selected *Asset
	for i := range release.Assets {
		a := &release.Assets[i]
		if a.OS == target.OS && a.Arch == target.Arch {
			if selected != nil {
				return errors.New("release contains duplicate assets for runtime target")
			}
			selected = a
		}
	}
	if selected == nil {
		return fmt.Errorf("release has no asset for %s/%s", target.OS, target.Arch)
	}
	if err := validateAsset(*selected, release.source); err != nil {
		return err
	}
	client, err := newHTTPClient(release.source)
	if err != nil {
		return err
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, selected.URL, nil)
	if err != nil {
		return fmt.Errorf("create asset request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download update asset: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download update asset: unexpected HTTP status %s", resp.Status)
	}
	if resp.ContentLength > maxAssetSize || (selected.Size > 0 && resp.ContentLength >= 0 && resp.ContentLength != selected.Size) {
		return errors.New("update asset size does not match manifest or exceeds limit")
	}
	if selected.Size <= 0 || selected.Size > maxAssetSize {
		return errors.New("update asset size is invalid or exceeds limit")
	}
	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, ".convertgo-update-*")
	if err != nil {
		return fmt.Errorf("create update temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		if retErr != nil {
			_ = os.Remove(tmpName)
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, maxAssetSize+1))
	if err != nil {
		return fmt.Errorf("write update asset: %w", err)
	}
	if n > maxAssetSize || n != selected.Size {
		return errors.New("update asset size does not match manifest or exceeds limit")
	}
	want, _ := hexDecode(selected.SHA256)
	if len(want) != sha256.Size || subtle.ConstantTimeCompare(h.Sum(nil), want) != 1 {
		return errors.New("update asset SHA-256 does not match manifest")
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync update asset: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close update asset: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("publish update asset: %w", err)
	}
	return nil
}

func githubManifestURL(ctx context.Context, client *http.Client, repository string) (string, error) {
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || !validRepoPart(parts[0]) || !validRepoPart(parts[1]) {
		return "", errors.New("repository must be owner/repository")
	}
	apiURL := "https://api.github.com/repos/" + parts[0] + "/" + parts[1] + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch GitHub latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch GitHub latest release: unexpected HTTP status %s", resp.Status)
	}
	body, err := readBounded(resp.Body, maxManifestSize)
	if err != nil {
		return "", fmt.Errorf("read GitHub release metadata: %w", err)
	}
	var release struct {
		Assets []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	// GitHub's API evolves, so tolerate unknown fields while decoding its envelope.
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&release); err != nil {
		return "", fmt.Errorf("decode GitHub release metadata: %w", err)
	}
	for _, asset := range release.Assets {
		if asset.Name == manifestName {
			return asset.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("GitHub release has no %q asset", manifestName)
}

func validRepoPart(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return s != "." && s != ".."
}

func validateSource(s UpdateSource) error {
	if s.usesGitHub() {
		if s.MirrorURL != "" {
			return errors.New("GitHub update source cannot include a mirror URL")
		}
		return nil
	}
	if s.Kind != SourceMirror {
		return fmt.Errorf("unsupported update source %q", s.Kind)
	}
	u, err := parseHTTPSURL(s.MirrorURL)
	if err != nil {
		return fmt.Errorf("invalid mirror URL: %w", err)
	}
	if u.RawQuery != "" || u.ForceQuery {
		return errors.New("mirror URL query is not allowed")
	}
	return nil
}

func validateAsset(a Asset, source UpdateSource) error {
	if a.OS == "" || a.Arch == "" || a.Filename == "" || strings.ContainsAny(a.Filename, "/\\\x00") {
		return errors.New("manifest contains invalid asset identity")
	}
	if a.Size <= 0 || a.Size > maxAssetSize {
		return errors.New("manifest asset size is invalid or exceeds limit")
	}
	if len(a.SHA256) != 64 {
		return errors.New("manifest asset SHA-256 is invalid")
	}
	if _, err := hexDecode(a.SHA256); err != nil {
		return errors.New("manifest asset SHA-256 is invalid")
	}
	u, err := parseHTTPSURL(a.URL)
	if err != nil {
		return fmt.Errorf("manifest asset URL: %w", err)
	}
	if !sourceAllowsURL(u, source) {
		return fmt.Errorf("manifest asset host %q is outside the selected source", u.Host)
	}
	return nil
}

func parseHTTPSURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" || u.Host == "" || u.User != nil || u.Opaque != "" {
		return nil, errors.New("absolute HTTPS URL without credentials required")
	}
	if u.Fragment != "" || strings.Contains(raw, "#") {
		return nil, errors.New("URL fragments are not allowed")
	}
	if _, _, err := net.SplitHostPort(u.Host); err != nil && strings.Contains(u.Host, ":") {
		return nil, errors.New("invalid URL host")
	}
	return u, nil
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}

func approvedGitHubHost(host string) bool {
	switch strings.ToLower(host) {
	case "api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
		return true
	default:
		return false
	}
}

func approvedGitHubURL(u *url.URL) bool {
	return approvedGitHubHost(u.Hostname()) && (u.Port() == "" || u.Port() == "443")
}

func sourceAllowsURL(u *url.URL, source UpdateSource) bool {
	if source.usesGitHub() {
		return approvedGitHubURL(u)
	}
	origin, err := parseHTTPSURL(source.MirrorURL)
	return err == nil && (sameOrigin(u, origin) || approvedGitHubURL(u))
}

func newHTTPClient(source UpdateSource) (*http.Client, error) {
	if err := validateSource(source); err != nil {
		return nil, err
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}).DialContext, TLSHandshakeTimeout: 8 * time.Second, ResponseHeaderTimeout: 12 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 8, MaxIdleConnsPerHost: 2, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: source.rootCAs}}
	return &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many update redirects")
		}
		if req.URL.Scheme != "https" || req.URL.User != nil {
			return errors.New("redirect URL must use HTTPS without credentials")
		}
		if !sourceAllowsURL(req.URL, source) {
			return errors.New("redirect leaves the selected update hosts")
		}
		return nil
	}}, nil
}

func decodeManifest(b []byte) (Manifest, error) {
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("decode update manifest: %w", err)
	}
	if err := ensureJSONEOF(dec); err != nil {
		return Manifest{}, err
	}
	if len(m.Assets) == 0 || m.Signature == "" {
		return Manifest{}, errors.New("update manifest is missing required fields")
	}
	if !releaseVersionPattern.MatchString(m.Version) {
		return Manifest{}, errors.New("update manifest version is invalid")
	}
	return m, nil
}

func ensureJSONEOF(dec *json.Decoder) error {
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("update manifest contains trailing data")
	}
	return nil
}

func marshalManifest(m Manifest, omitSignature bool) ([]byte, error) {
	assets := append([]Asset(nil), m.Assets...)
	sort.Slice(assets, func(i, j int) bool {
		a, b := assets[i], assets[j]
		if a.OS != b.OS {
			return a.OS < b.OS
		}
		if a.Arch != b.Arch {
			return a.Arch < b.Arch
		}
		return a.Filename < b.Filename
	})
	value := struct {
		Version   string  `json:"version"`
		Assets    []Asset `json:"assets"`
		Signature string  `json:"signature,omitempty"`
	}{Version: m.Version, Assets: assets}
	if !omitSignature {
		value.Signature = m.Signature
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), nil
}

func signManifest(m Manifest, private ed25519.PrivateKey) ([]byte, error) {
	b, err := marshalManifest(m, true)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(private, b), nil
}

func verifyManifest(m Manifest, public ed25519.PublicKey) error {
	if !releaseVersionPattern.MatchString(m.Version) {
		return errors.New("update manifest version is invalid")
	}
	if len(public) != ed25519.PublicKeySize {
		return errors.New("update signing public key is invalid")
	}
	sig, err := base64.StdEncoding.DecodeString(m.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("update manifest signature is invalid")
	}
	b, err := marshalManifest(m, true)
	if err != nil {
		return err
	}
	if !ed25519.Verify(public, b, sig) {
		return errors.New("update manifest signature verification failed")
	}
	return nil
}

func productionPublicKey() (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(productionPublicKeyPEM))
	if block == nil {
		return nil, errors.New("embedded update public key is invalid")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse embedded update public key: %w", err)
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("embedded update key is not Ed25519")
	}
	return pub, nil
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("response exceeds size limit")
	}
	return b, nil
}

func hexDecode(s string) ([]byte, error) {
	if len(s) != sha256.Size*2 {
		return nil, errors.New("invalid SHA-256 hex")
	}
	return hex.DecodeString(s)
}
