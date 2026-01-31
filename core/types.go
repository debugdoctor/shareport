package core

type Combo struct {
	Name        string
	Protocol    string
	Network     string
	Security    string
	WithTLS     bool
	WithReality bool
}

type InboundSelection struct {
	Combo      Combo
	ListenPort string
	InboundTag string
	// UserID is used by UUID-based inbounds (e.g. VLESS).
	UserID string
	// Password is used by password-based inbounds (e.g. Trojan).
	Password string
	SNI      string
	// RealityServerNames is only used for server-side REALITY.
	// Xray client-side uses singular serverName, but server-side supports a list.
	RealityServerNames []string
	WSPath             string
	HTTPHost           string
	HTTPPath           string
	XHTTPHost          string
	XHTTPPath          string
	XHTTPMode          string
	Dest               string
	RealityKey         string
	ShortIDs           []string
	TLSCert            string
	TLSKey             string
}
