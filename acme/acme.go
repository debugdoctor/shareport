package acme

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"aimerick.com/shareport/i18n"
	"aimerick.com/shareport/ui"
	"github.com/go-acme/lego/v4/certcrypto"
	"github.com/go-acme/lego/v4/certificate"
	"github.com/go-acme/lego/v4/challenge/http01"
	"github.com/go-acme/lego/v4/lego"
	"github.com/go-acme/lego/v4/providers/dns/manual"
	"github.com/go-acme/lego/v4/registration"
)

const (
	tlsChallengeHTTP = 1
	tlsChallengeDNS  = 2
)

type acmeUser struct {
	Email        string
	Registration *registration.Resource
	Key          *ecdsa.PrivateKey
}

func (u *acmeUser) GetEmail() string {
	return u.Email
}

func (u *acmeUser) GetRegistration() *registration.Resource {
	return u.Registration
}

func (u *acmeUser) GetPrivateKey() crypto.PrivateKey {
	return u.Key
}

func EnsureCertificate(ui *ui.TUI, msgs i18n.Messages, domain, email string, challenge int, dnsProvider string) (string, string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return "", "", fmt.Errorf("empty domain")
	}
	if email == "" {
		return "", "", fmt.Errorf("empty email")
	}

	baseDir := filepath.Join(".", ".shareport", "certs", domain)
	certPath := filepath.Join(baseDir, "fullchain.pem")
	keyPath := filepath.Join(baseDir, "privkey.pem")
	if fileExists(certPath) && fileExists(keyPath) {
		return certPath, keyPath, nil
	}

	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return "", "", err
	}

	user, created, err := loadOrCreateACMEUser(email)
	if err != nil {
		return "", "", err
	}

	config := lego.NewConfig(user)
	config.CADirURL = lego.LEDirectoryProduction
	config.Certificate.KeyType = certcrypto.RSA2048

	client, err := lego.NewClient(config)
	if err != nil {
		return "", "", err
	}

	switch challenge {
	case tlsChallengeDNS:
		if providerName := strings.TrimSpace(dnsProvider); providerName != "" && providerName != "manual" {
			return "", "", fmt.Errorf("unsupported dns provider: %s", providerName)
		}
		dnsProvider, err := manual.NewDNSProvider()
		if err != nil {
			return "", "", err
		}
		if err := client.Challenge.SetDNS01Provider(dnsProvider); err != nil {
			return "", "", err
		}
	default:
		httpProvider := http01.NewProviderServer("", "80")
		if err := client.Challenge.SetHTTP01Provider(httpProvider); err != nil {
			return "", "", err
		}
	}

	if user.Registration == nil {
		if !created {
			reg, err := client.Registration.ResolveAccountByKey()
			if err == nil {
				user.Registration = reg
			}
		}
		if user.Registration == nil {
			reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
			if err != nil {
				return "", "", err
			}
			user.Registration = reg
		}
		if err := saveACMEUser(user); err != nil {
			return "", "", err
		}
	}

	request := certificate.ObtainRequest{
		Domains: []string{domain},
		Bundle:  true,
	}
	res, err := client.Certificate.Obtain(request)
	if err != nil {
		return "", "", err
	}

	if err := os.WriteFile(certPath, res.Certificate, 0o600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyPath, res.PrivateKey, 0o600); err != nil {
		return "", "", err
	}
	if err := writeCertMeta(baseDir, certMeta{
		Domain:      domain,
		Email:       email,
		Challenge:   challengeName(challenge),
		DNSProvider: dnsProvider,
	}); err != nil {
		return "", "", err
	}

	ui.Println(msgs.Get("tls_auto_saved"))
	return certPath, keyPath, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

type certMeta struct {
	Domain      string `json:"domain"`
	Email       string `json:"email"`
	Challenge   string `json:"challenge"`
	DNSProvider string `json:"dns_provider"`
}

func writeCertMeta(baseDir string, meta certMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(baseDir, "meta.json"), data, 0o600)
}

func readCertMeta(path string) (certMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return certMeta{}, err
	}
	var meta certMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return certMeta{}, err
	}
	return meta, nil
}

func challengeName(challenge int) string {
	switch challenge {
	case tlsChallengeDNS:
		return "dns-01"
	default:
		return "http-01"
	}
}

func challengeFromName(name string) int {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "dns-01", "dns":
		return tlsChallengeDNS
	default:
		return tlsChallengeHTTP
	}
}

func loadOrCreateACMEUser(email string) (*acmeUser, bool, error) {
	if email == "" {
		return nil, false, fmt.Errorf("empty email")
	}
	keyPath, regPath := acmeAccountPaths(email)
	if fileExists(keyPath) {
		key, err := readECPrivateKey(keyPath)
		if err != nil {
			return nil, false, err
		}
		user := &acmeUser{Email: email, Key: key}
		if fileExists(regPath) {
			data, err := os.ReadFile(regPath)
			if err != nil {
				return nil, false, err
			}
			var reg registration.Resource
			if err := json.Unmarshal(data, &reg); err != nil {
				return nil, false, err
			}
			user.Registration = &reg
		}
		return user, false, nil
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, false, err
	}
	return &acmeUser{Email: email, Key: key}, true, nil
}

func saveACMEUser(user *acmeUser) error {
	if user == nil || user.Key == nil {
		return fmt.Errorf("missing user key")
	}
	keyPath, regPath := acmeAccountPaths(user.Email)
	if err := os.MkdirAll(filepath.Dir(keyPath), 0o700); err != nil {
		return err
	}
	if err := writeECPrivateKey(keyPath, user.Key); err != nil {
		return err
	}
	if user.Registration != nil {
		data, err := json.MarshalIndent(user.Registration, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(regPath, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func acmeAccountPaths(email string) (string, string) {
	safe := strings.ReplaceAll(urlEscape(email), "%", "_")
	base := filepath.Join(".shareport", "acme", "accounts", safe)
	return base + ".key", base + ".json"
}

func readECPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("invalid pem")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

func writeECPrivateKey(path string, key *ecdsa.PrivateKey) error {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	block := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	return os.WriteFile(path, pem.EncodeToMemory(block), 0o600)
}

func urlEscape(s string) string {
	replacer := strings.NewReplacer(
		"@", "_at_",
		"+", "_plus_",
		"/", "_",
		"\\", "_",
		":", "_",
	)
	return replacer.Replace(s)
}

func RenewCertificates(ui *ui.TUI, msgs i18n.Messages) error {
	base := filepath.Join(".shareport", "certs")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Println(msgs.Get("tls_renew_none"))
			return nil
		}
		return err
	}

	renewedAny := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		domainDir := filepath.Join(base, entry.Name())
		metaPath := filepath.Join(domainDir, "meta.json")
		if !fileExists(metaPath) {
			continue
		}
		meta, err := readCertMeta(metaPath)
		if err != nil {
			ui.Println(fmt.Sprintf("%s %s", msgs.Get("tls_renew_meta_failed"), entry.Name()))
			continue
		}
		if err := renewCertificate(ui, msgs, domainDir, meta); err != nil {
			ui.Println(fmt.Sprintf("%s %s: %v", msgs.Get("tls_renew_failed"), meta.Domain, err))
			continue
		}
		renewedAny = true
		ui.Println(fmt.Sprintf("%s %s", msgs.Get("tls_renew_ok"), meta.Domain))
	}
	if !renewedAny {
		ui.Println(msgs.Get("tls_renew_none"))
	}
	return nil
}

func renewCertificate(ui *ui.TUI, msgs i18n.Messages, baseDir string, meta certMeta) error {
	certPath := filepath.Join(baseDir, "fullchain.pem")
	keyPath := filepath.Join(baseDir, "privkey.pem")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}

	user, created, err := loadOrCreateACMEUser(meta.Email)
	if err != nil {
		return err
	}
	config := lego.NewConfig(user)
	config.CADirURL = lego.LEDirectoryProduction
	config.Certificate.KeyType = certcrypto.RSA2048
	client, err := lego.NewClient(config)
	if err != nil {
		return err
	}

	challenge := challengeFromName(meta.Challenge)
	switch challenge {
	case tlsChallengeDNS:
		if providerName := strings.TrimSpace(meta.DNSProvider); providerName != "" && providerName != "manual" {
			return fmt.Errorf("unsupported dns provider: %s", providerName)
		}
		dnsProvider, err := manual.NewDNSProvider()
		if err != nil {
			return err
		}
		if err := client.Challenge.SetDNS01Provider(dnsProvider); err != nil {
			return err
		}
	default:
		ui.Println(msgs.Get("tls_http_notice"))
		ui.Println(msgs.Get("tls_http_notice_root"))
		httpProvider := http01.NewProviderServer("", "80")
		if err := client.Challenge.SetHTTP01Provider(httpProvider); err != nil {
			return err
		}
	}

	if user.Registration == nil {
		if !created {
			reg, err := client.Registration.ResolveAccountByKey()
			if err == nil {
				user.Registration = reg
			}
		}
		if user.Registration == nil {
			reg, err := client.Registration.Register(registration.RegisterOptions{TermsOfServiceAgreed: true})
			if err != nil {
				return err
			}
			user.Registration = reg
		}
		if err := saveACMEUser(user); err != nil {
			return err
		}
	}

	res, err := client.Certificate.RenewWithOptions(certificate.Resource{
		Domain:      meta.Domain,
		Certificate: certPEM,
		PrivateKey:  keyPEM,
	}, &certificate.RenewOptions{Bundle: true})
	if err != nil {
		return err
	}

	if err := os.WriteFile(certPath, res.Certificate, 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, res.PrivateKey, 0o600); err != nil {
		return err
	}
	return nil
}

func PrintDNSProviderList(ui *ui.TUI) {
	names := dnsProviderNames()
	const perLine = 4
	for i := 0; i < len(names); i += perLine {
		end := i + perLine
		if end > len(names) {
			end = len(names)
		}
		ui.Println(strings.Join(names[i:end], "  "))
	}
}

func dnsProviderNames() []string {
	return []string{
		"acme-dns",
		"alidns",
		"allinkl",
		"arvancloud",
		"auroradns",
		"autodns",
		"azure",
		"azuredns",
		"bindman",
		"bluecat",
		"brandit",
		"bunny",
		"checkdomain",
		"civo",
		"clouddns",
		"cloudflare",
		"cloudns",
		"cloudru",
		"cloudxns",
		"conoha",
		"constellix",
		"cpanel",
		"derak",
		"desec",
		"designate",
		"digitalocean",
		"dnshomede",
		"dnsimple",
		"dnsmadeeasy",
		"dnspod",
		"dode",
		"domeneshop",
		"dreamhost",
		"duckdns",
		"dyn",
		"dynu",
		"easydns",
		"edgedns",
		"efficientip",
		"epik",
		"exec",
		"exoscale",
		"freemyip",
		"gandi",
		"gandiv5",
		"gcloud",
		"gcore",
		"glesys",
		"godaddy",
		"googledomains",
		"hetzner",
		"hostingde",
		"hosttech",
		"httpnet",
		"httpreq",
		"hurricane",
		"hyperone",
		"ibmcloud",
		"iij",
		"iijdpf",
		"infoblox",
		"infomaniak",
		"internetbs",
		"inwx",
		"ionos",
		"ipv64",
		"iwantmyname",
		"joker",
		"liara",
		"lightsail",
		"linode",
		"liquidweb",
		"loopia",
		"luadns",
		"mailinabox",
		"manual",
		"metaname",
		"mydnsjp",
		"mythicbeasts",
		"namecheap",
		"namedotcom",
		"namesilo",
		"nearlyfreespeech",
		"netcup",
		"netlify",
		"nicmanager",
		"nifcloud",
		"njalla",
		"nodion",
		"ns1",
		"oraclecloud",
		"otc",
		"ovh",
		"pdns",
		"plesk",
		"porkbun",
		"rackspace",
		"rcodezero",
		"regru",
		"rfc2136",
		"rimuhosting",
		"route53",
		"safedns",
		"sakuracloud",
		"scaleway",
		"selectel",
		"selectelv2",
		"servercow",
		"shellrent",
		"simply",
		"sonic",
		"stackpath",
		"tencentcloud",
		"transip",
		"ultradns",
		"variomedia",
		"vegadns",
		"vercel",
		"versio",
		"vinyldns",
		"vkcloud",
		"vscale",
		"vultr",
		"webnames",
		"websupport",
		"wedos",
		"yandex",
		"yandex360",
		"yandexcloud",
		"zoneee",
		"zonomi",
	}
}
