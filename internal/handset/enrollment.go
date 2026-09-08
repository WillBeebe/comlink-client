package handset

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"time"

	"comlink/internal/admission"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"nexum/circuit"
)

// PublicProfile creates endpoint keys before the first network operation. A
// failed enrollment can be retried with the same keys and assigned number.
func PublicProfile(path, rootsFile, audience string) (Config, error) {
	rootsRaw, err := os.ReadFile(rootsFile)
	var roots map[string]string
	if err != nil || len(rootsRaw) > 65536 || json.Unmarshal(rootsRaw, &roots) != nil || len(roots) == 0 {
		return Config{}, errors.New("pinned public roots required")
	}
	var cfg Config
	st, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg.Identity, err = circuit.NewIdentity()
		if err != nil {
			return cfg, err
		}
		cfg.Roots = roots
		cfg.Audience = audience
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return cfg, err
		}
		raw, _ := json.Marshal(cfg)
		f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return cfg, e
		}
		_, err = f.Write(raw)
		if err == nil {
			err = f.Sync()
		}
		e = f.Close()
		if err == nil {
			err = e
		}
		if err == nil {
			err = syncDirectory(filepath.Dir(path))
		}
		return cfg, err
	}
	if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 65536 {
		return cfg, errors.New("private public-enrollment profile required")
	}
	raw, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(raw, &cfg) != nil {
		return cfg, errors.New("invalid enrollment profile")
	}
	if _, err = cfg.Identity.Public(); err != nil || cfg.Audience != audience || !equal(cfg.Roots, roots) {
		return cfg, errors.New("profile authority differs from configured resource")
	}
	return cfg, nil
}
func saveProfile(path string, cfg Config) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".comlink-profile-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}
func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func equal(a, b any) bool { x, _ := json.Marshal(a); y, _ := json.Marshal(b); return bytes.Equal(x, y) }
func canonicalPolicy(p admission.CallPolicy) admission.CallPolicy {
	p.Allowed = append([]string{}, p.Allowed...)
	p.Blocked = append([]string{}, p.Blocked...)
	sort.Strings(p.Allowed)
	sort.Strings(p.Blocked)
	return p
}

// EnrollmentOperation returns a short-lived token only to the calling endpoint
// process. It does not write tokens or issuer keys to the profile or stdout.
func EnrollmentOperation(ctx context.Context, cfg Config, endpoint, ca, purpose, subscriber string, policy *admission.CallPolicy) (admission.Enrollment, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/mcp" || (u.Scheme != "https" && (u.Scheme != "http" || !net.ParseIP(u.Hostname()).IsLoopback())) {
		return admission.Enrollment{}, errors.New("HTTPS /mcp resource required")
	}
	u.Path = "/enroll/mcp"
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS13}
	if ca != "" {
		raw, e := os.ReadFile(ca)
		if e != nil {
			return admission.Enrollment{}, e
		}
		roots, e := x509.SystemCertPool()
		if e != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(raw) {
			return admission.Enrollment{}, errors.New("invalid CA")
		}
		transport.TLSClientConfig.RootCAs = roots
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "comlink-enrollment-client", Version: "1.0.0"}, &mcp.ClientOptions{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: u.String(), HTTPClient: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}}, nil)
	if err != nil {
		return admission.Enrollment{}, errors.New("enrollment connection failed")
	}
	defer session.Close()
	call := func(name string, in, out any) error {
		r, e := session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: in})
		if e != nil || r.IsError {
			return admission.ErrDenied
		}
		raw, e := json.Marshal(r.StructuredContent)
		if e != nil || len(raw) > 65536 || json.Unmarshal(raw, out) != nil {
			return admission.ErrDenied
		}
		return nil
	}
	if policy != nil {
		p := canonicalPolicy(*policy)
		policy = &p
	}
	request, err := admission.Request(cfg.Identity, purpose, subscriber, endpoint, policy, time.Now())
	if err != nil {
		return admission.Enrollment{}, err
	}
	var challenge admission.Challenge
	if err = call("identity.challenge", request, &challenge); err != nil {
		return admission.Enrollment{}, err
	}
	if challenge.Purpose != purpose || (subscriber != "" && challenge.SubscriberID != subscriber) || !equal(challenge.Policy, policy) {
		return admission.Enrollment{}, admission.ErrDenied
	}
	if cfg.Credential.Number != "" && purpose != "suspend" && !equal(challenge.Credential, cfg.Credential) {
		return admission.Enrollment{}, admission.ErrDenied
	}
	proof, err := admission.Approve(cfg.Identity, challenge, endpoint, cfg.Roots, time.Now())
	if err != nil {
		return admission.Enrollment{}, err
	}
	var result admission.Enrollment
	if err = call("identity.complete", proof, &result); err != nil {
		return result, err
	}
	if result.SubscriberID != challenge.SubscriberID || !equal(result.Credential, challenge.Credential) || !result.Policy.Valid() {
		return admission.Enrollment{}, admission.ErrDenied
	}
	if challenge.Binding != nil {
		expected, e := challenge.Binding.WithSignature(challenge.Owner.ID(), proof.BindingSignature)
		if e != nil || !equal(expected, result.Binding) || result.Revoked {
			return admission.Enrollment{}, admission.ErrDenied
		}
	}
	if policy != nil && !equal(result.Policy, *policy) {
		return admission.Enrollment{}, admission.ErrDenied
	}
	if purpose == "revoke" || purpose == "suspend" {
		if !result.Revoked || result.AccessToken != "" {
			return admission.Enrollment{}, admission.ErrDenied
		}
	} else if purpose == "enroll" || purpose == "access" {
		token, e := hex.DecodeString(result.AccessToken)
		if e != nil || len(token) != 32 || !time.Now().Before(result.ExpiresAt) || result.ExpiresAt.After(time.Now().Add(admission.AccessTTL+5*time.Second)) {
			return admission.Enrollment{}, admission.ErrDenied
		}
	}
	return result, nil
}
func Enroll(ctx context.Context, path, roots, endpoint, ca string) (Config, string, error) {
	cfg, err := PublicProfile(path, roots, endpoint)
	if err != nil {
		return cfg, "", err
	}
	purpose := "access"
	if cfg.SubscriberID == "" {
		purpose = "enroll"
	}
	out, err := EnrollmentOperation(ctx, cfg, endpoint, ca, purpose, cfg.SubscriberID, nil)
	if err != nil {
		return cfg, "", err
	}
	cfg.SubscriberID = out.SubscriberID
	cfg.Credential = out.Credential
	if err = saveProfile(path, cfg); err != nil {
		return cfg, "", err
	}
	return cfg, out.AccessToken, nil
}
func ServePublic(ctx context.Context, path, roots, endpoint, ca string) error {
	var release func()
	defer func() {
		if release != nil {
			release()
		}
	}()
	return serveProvider(ctx, func(ctx context.Context) (Config, string, error) {
		if release == nil {
			var err error
			release, err = leaseProfile(path)
			if err != nil {
				return Config{}, "", err
			}
		}
		return Enroll(ctx, path, roots, endpoint, ca)
	}, endpoint, ca, &mcp.IOTransport{Reader: os.Stdin, Writer: os.Stdout}, path)
}
