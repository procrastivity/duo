package portablelauncher

// Pinned suite identities and canonical payload digests.
const (
	DuoSourceCommit      = "1437f2f69e71d164b4248ed121803e53e3e1a2ba"
	SkillName            = "duo-delegation-loop"
	SkillFormat          = "duo.skill/duo-delegation-loop/v1"
	SkillContentDigest   = "sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182"
	ExternalSchemaDigest = "sha256:8b055ff60f0ca5f3da46302671591aa0dae61185cd9f1f15fcceb36d841e7497"
	ConfigSchemaDigest   = "sha256:d1a2fd1be5339c5699a21cf65616a4d09ff932a5495111d02a422b7656086842"
	HostSchemaDigest     = "sha256:c48f1f54ee0150ca27e11fd44455fe94aeadb20fdf4e4a62393ed822a4e5b150"
	DeliveryAssetDigest  = "sha256:2708a3435821fb2e72302ee56d8a07fe62c265214ae8fd342e385c03ef245a23"
	RequestDigest        = "sha256:b0a32f59630d6398da8d8913744dd7cd50a313d86bf7e833309aad0d47cc9c3b"
	ReplyDigest          = "sha256:b3300f2f6e0338d2b672891be66827d95779a2eabf4a75dcae1b6f56c9a4dc98"
	PromptDigest         = "sha256:b40d3a68eb33083b92199d734da2dece28395a6acc6d5e2cb9d7032dae4d11c7"
	ConflictDigest       = "sha256:dbe904a42a9d9d9cdf7908508fa393ce43c9c53ea16a8cb8c9b82e7005f602bc"
)

// AcceptedLauncher is an exact launcher version and executable digest admitted
// by the suite.
type AcceptedLauncher struct {
	Version string `json:"version"`
	Digest  string `json:"executable_sha256"`
}

var acceptedLaunchers = map[string]AcceptedLauncher{
	"amp":      {Version: "0.0.1789675234-g2899fe", Digest: "sha256:f351217dac739614b4d9b3eacad728ce6d61647ab1f5d4a4de44d99aba6598ee"},
	"opencode": {Version: "1.18.31", Digest: "sha256:f9dab32248695e9ebd56b16a1921798fd85112cf5a69c7dfd0cabc1e17be4a11"},
	"codex":    {Version: "0.154.0", Digest: "sha256:3188814c35471432d4123203e0eb38e5bddc60226e3d7ddf0e59e649ea140022"},
}

// AcceptedLauncherPin returns the exact admitted pin for a launcher name.
func AcceptedLauncherPin(name string) (AcceptedLauncher, bool) {
	p, ok := acceptedLaunchers[name]
	return p, ok
}
