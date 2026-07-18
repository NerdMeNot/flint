// Package awsec2 is Flint's AWS EC2 compute provider: on-demand and spot
// machines created per the fleet manager's decisions, bootstrapped via
// cloud-init user data (agent download → runtime bundle → systemd unit →
// register with the one-time token). No AMI baking: stock AL2023 via SSM
// parameter lookup keeps the machine images zero-maintenance.
package awsec2

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/pkg/compute"
)

func init() {
	compute.Register("aws", func(ctx context.Context, name string, config json.RawMessage, creds []byte) (compute.Provider, error) {
		return New(ctx, name, config, creds)
	})
}

// Config is the provider's stored configuration (compute_providers.config).
type Config struct {
	Region           string   `json:"region"`
	SubnetIDs        []string `json:"subnetIds"`
	SecurityGroupIDs []string `json:"securityGroupIds"`
	// InstanceProfile is the IAM instance profile name machines run with.
	InstanceProfile string `json:"instanceProfile,omitempty"`
	// AMI: a fixed id, or empty for the stock AL2023 SSM lookup per arch.
	AMIID   string `json:"amiId,omitempty"`
	KeyName string `json:"keyName,omitempty"`
	// SpotMaxPricePct caps spot bids as a percentage of on-demand (default
	// 100: pay at most on-demand).
	SpotMaxPricePct int `json:"spotMaxPricePct,omitempty"`
}

// credentialsJSON is the optional stored credential blob (most installs use
// the ambient chain instead).
type credentialsJSON struct {
	AccessKeyID     string `json:"accessKeyId"`
	SecretAccessKey string `json:"secretAccessKey"`
	SessionToken    string `json:"sessionToken,omitempty"`
}

// ec2API is the slice of the EC2 API the provider uses — mockable in tests.
type ec2API interface {
	RunInstances(ctx context.Context, params *ec2.RunInstancesInput, optFns ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error)
	TerminateInstances(ctx context.Context, params *ec2.TerminateInstancesInput, optFns ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error)
	DescribeInstances(ctx context.Context, params *ec2.DescribeInstancesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error)
	DescribeSpotPriceHistory(ctx context.Context, params *ec2.DescribeSpotPriceHistoryInput, optFns ...func(*ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error)
	DescribeImages(ctx context.Context, params *ec2.DescribeImagesInput, optFns ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error)
}

// Provider implements compute.Provider on EC2.
type Provider struct {
	name string
	cfg  Config
	api  ec2API

	spotMu     sync.Mutex
	spotCache  map[string]spotQuote // instance type → cached price
	subnetNext int
}

type spotQuote struct {
	price    float64
	cachedAt time.Time
}

const spotCacheTTL = 5 * time.Minute

// New constructs the provider from stored config + optional credentials.
func New(ctx context.Context, name string, rawConfig json.RawMessage, rawCreds []byte) (*Provider, error) {
	var cfg Config
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return nil, fmt.Errorf("awsec2: invalid config: %w", err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("awsec2: region is required")
	}
	if len(cfg.SubnetIDs) == 0 {
		return nil, fmt.Errorf("awsec2: at least one subnetId is required")
	}

	opts := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(cfg.Region)}
	if len(rawCreds) > 0 {
		var c credentialsJSON
		if err := json.Unmarshal(rawCreds, &c); err != nil {
			return nil, fmt.Errorf("awsec2: invalid credentials blob: %w", err)
		}
		opts = append(opts, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, c.SessionToken)))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("awsec2: aws config: %w", err)
	}
	return &Provider{
		name:      name,
		cfg:       cfg,
		api:       ec2.NewFromConfig(awsCfg),
		spotCache: map[string]spotQuote{},
	}, nil
}

// newWithAPI builds a provider over a mock API (tests).
func newWithAPI(name string, cfg Config, api ec2API) *Provider {
	return &Provider{name: name, cfg: cfg, api: api, spotCache: map[string]spotQuote{}}
}

func (p *Provider) Name() string { return p.name }

// Classes: EC2 supplies both stable (on-demand) and interruptible (spot).
func (p *Provider) Classes() []compute.CapacityType {
	return []compute.CapacityType{compute.CapacityOnDemand, compute.CapacitySpot}
}

// Quote prices catalog types satisfying the requirements: on-demand from the
// embedded baseline, spot from live DescribeSpotPriceHistory (5-minute cache).
func (p *Provider) Quote(ctx context.Context, req compute.Requirements) ([]compute.Offer, error) {
	var offers []compute.Offer
	now := time.Now()
	for _, entry := range catalog {
		if entry.CPUMillis < req.CPUMillis || entry.MemoryMB < req.MemoryMB {
			continue
		}
		if req.Arch != "" && entry.Arch != req.Arch {
			continue
		}
		if req.MaxBootSeconds > 0 && entry.BootSeconds > req.MaxBootSeconds {
			continue
		}
		if len(req.InstanceTypes) > 0 && !slices.Contains(req.InstanceTypes, entry.InstanceType) {
			continue
		}
		if req.GPU != nil {
			continue // GPU shapes are a catalog follow-up
		}

		base := compute.Offer{
			Provider: p.name, InstanceType: entry.InstanceType, Region: p.cfg.Region,
			CPUMillis: entry.CPUMillis, MemoryMB: entry.MemoryMB, DiskGB: 0,
			Arch: entry.Arch, ExpectedBootSeconds: entry.BootSeconds,
			ExpiresAt: now.Add(spotCacheTTL),
		}
		wantOD := req.Capacity == compute.CapacityOnDemand || req.Capacity == compute.CapacityAny || req.Capacity == ""
		wantSpot := req.Capacity == compute.CapacitySpot || req.Capacity == compute.CapacityAny

		if wantOD {
			od := base
			od.Capacity = compute.CapacityOnDemand
			od.PricePerHourUSD = entry.OnDemandUSD
			offers = append(offers, od)
		}
		if wantSpot {
			price, err := p.spotPrice(ctx, entry)
			if err == nil && price > 0 {
				spot := base
				spot.Capacity = compute.CapacitySpot
				spot.PricePerHourUSD = price
				spot.InterruptionRisk = entry.InterruptionPct
				offers = append(offers, spot)
			}
		}
	}
	return offers, nil
}

// spotPrice returns the recent spot price for a type (cached).
func (p *Provider) spotPrice(ctx context.Context, entry catalogEntry) (float64, error) {
	p.spotMu.Lock()
	if q, ok := p.spotCache[entry.InstanceType]; ok && time.Since(q.cachedAt) < spotCacheTTL {
		p.spotMu.Unlock()
		return q.price, nil
	}
	p.spotMu.Unlock()

	out, err := p.api.DescribeSpotPriceHistory(ctx, &ec2.DescribeSpotPriceHistoryInput{
		InstanceTypes:       []ec2types.InstanceType{ec2types.InstanceType(entry.InstanceType)},
		ProductDescriptions: []string{"Linux/UNIX"},
		StartTime:           awssdk.Time(time.Now().Add(-time.Hour)),
		MaxResults:          awssdk.Int32(10),
	})
	if err != nil {
		return 0, err
	}
	best := 0.0
	for _, sp := range out.SpotPriceHistory {
		var v float64
		if _, err := fmt.Sscanf(awssdk.ToString(sp.SpotPrice), "%f", &v); err == nil && (best == 0 || v < best) {
			best = v
		}
	}
	p.spotMu.Lock()
	p.spotCache[entry.InstanceType] = spotQuote{price: best, cachedAt: time.Now()}
	p.spotMu.Unlock()
	return best, nil
}

// Create launches the instance with cloud-init bootstrap. ClientToken =
// bootstrap.MachineID makes retries idempotent.
func (p *Provider) Create(ctx context.Context, offer compute.Offer, bootstrap compute.Bootstrap) (compute.MachineRef, error) {
	amiID, err := p.resolveAMI(ctx, offer.Arch)
	if err != nil {
		return compute.MachineRef{}, err
	}
	userData := base64.StdEncoding.EncodeToString([]byte(cloudInit(bootstrap)))

	input := &ec2.RunInstancesInput{
		MinCount:     awssdk.Int32(1),
		MaxCount:     awssdk.Int32(1),
		ImageId:      awssdk.String(amiID),
		InstanceType: ec2types.InstanceType(offer.InstanceType),
		SubnetId:     awssdk.String(p.nextSubnet()),
		ClientToken:  awssdk.String(bootstrap.MachineID),
		UserData:     awssdk.String(userData),
		TagSpecifications: []ec2types.TagSpecification{{
			ResourceType: ec2types.ResourceTypeInstance,
			Tags: []ec2types.Tag{
				{Key: awssdk.String("flint:managed"), Value: awssdk.String("true")},
				{Key: awssdk.String("flint:machine-id"), Value: awssdk.String(bootstrap.MachineID)},
				{Key: awssdk.String("flint:pool"), Value: awssdk.String(bootstrap.PoolName)},
				{Key: awssdk.String("Name"), Value: awssdk.String("flint-" + bootstrap.PoolName)},
			},
		}},
	}
	if len(p.cfg.SecurityGroupIDs) > 0 {
		input.SecurityGroupIds = p.cfg.SecurityGroupIDs
	}
	if p.cfg.InstanceProfile != "" {
		input.IamInstanceProfile = &ec2types.IamInstanceProfileSpecification{
			Name: awssdk.String(p.cfg.InstanceProfile),
		}
	}
	if p.cfg.KeyName != "" {
		input.KeyName = awssdk.String(p.cfg.KeyName)
	}
	if offer.Capacity == compute.CapacitySpot {
		spot := &ec2types.SpotMarketOptions{
			SpotInstanceType:             ec2types.SpotInstanceTypeOneTime,
			InstanceInterruptionBehavior: ec2types.InstanceInterruptionBehaviorTerminate,
		}
		// Bid ceiling: on-demand (or the configured percentage of it).
		if pct := p.cfg.SpotMaxPricePct; pct > 0 && pct < 100 {
			if od := onDemandFor(offer.InstanceType); od > 0 {
				spot.MaxPrice = awssdk.String(fmt.Sprintf("%.4f", od*float64(pct)/100))
			}
		}
		input.InstanceMarketOptions = &ec2types.InstanceMarketOptionsRequest{
			MarketType: ec2types.MarketTypeSpot, SpotOptions: spot,
		}
	}

	out, err := p.api.RunInstances(ctx, input)
	if err != nil {
		return compute.MachineRef{}, fmt.Errorf("awsec2: RunInstances: %w", err)
	}
	if len(out.Instances) == 0 {
		return compute.MachineRef{}, fmt.Errorf("awsec2: RunInstances returned no instances")
	}
	id := awssdk.ToString(out.Instances[0].InstanceId)
	log.Info().Str("instance", id).Str("type", offer.InstanceType).
		Str("capacity", string(offer.Capacity)).Msg("awsec2: instance launched")
	return compute.MachineRef{Provider: p.name, ID: id, MachineID: bootstrap.MachineID, State: compute.RefPending}, nil
}

// Destroy terminates the instance; unknown ids are success.
func (p *Provider) Destroy(ctx context.Context, ref compute.MachineRef) error {
	if ref.ID == "" {
		return nil
	}
	_, err := p.api.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{ref.ID},
	})
	if err != nil && strings.Contains(err.Error(), "InvalidInstanceID") {
		return nil
	}
	return err
}

// List returns Flint-managed instances (tag flint:managed=true) in every
// non-terminated state — the reconciliation source of truth.
func (p *Provider) List(ctx context.Context) ([]compute.MachineRef, error) {
	out, err := p.api.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{Name: awssdk.String("tag:flint:managed"), Values: []string{"true"}},
		},
	})
	if err != nil {
		return nil, err
	}
	var refs []compute.MachineRef
	for _, res := range out.Reservations {
		for _, inst := range res.Instances {
			var machineID string
			for _, tag := range inst.Tags {
				if awssdk.ToString(tag.Key) == "flint:machine-id" {
					machineID = awssdk.ToString(tag.Value)
					break
				}
			}
			refs = append(refs, compute.MachineRef{
				Provider:  p.name,
				ID:        awssdk.ToString(inst.InstanceId),
				MachineID: machineID,
				State:     refState(inst.State),
			})
		}
	}
	return refs, nil
}

// resolveAMI returns the configured AMI or the newest stock AL2023 per arch.
func (p *Provider) resolveAMI(ctx context.Context, arch string) (string, error) {
	if p.cfg.AMIID != "" {
		return p.cfg.AMIID, nil
	}
	amiArch := "x86_64"
	if arch == "arm64" {
		amiArch = "arm64"
	}
	out, err := p.api.DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{"amazon"},
		Filters: []ec2types.Filter{
			{Name: awssdk.String("name"), Values: []string{"al2023-ami-2023*"}},
			{Name: awssdk.String("architecture"), Values: []string{amiArch}},
			{Name: awssdk.String("state"), Values: []string{"available"}},
		},
	})
	if err != nil {
		return "", fmt.Errorf("awsec2: AMI lookup: %w", err)
	}
	best := ""
	bestDate := ""
	for _, img := range out.Images {
		if d := awssdk.ToString(img.CreationDate); d > bestDate {
			bestDate = d
			best = awssdk.ToString(img.ImageId)
		}
	}
	if best == "" {
		return "", fmt.Errorf("awsec2: no AL2023 AMI found for %s", amiArch)
	}
	return best, nil
}

// nextSubnet round-robins configured subnets (AZ spread).
func (p *Provider) nextSubnet() string {
	p.spotMu.Lock()
	defer p.spotMu.Unlock()
	s := p.cfg.SubnetIDs[p.subnetNext%len(p.cfg.SubnetIDs)]
	p.subnetNext++
	return s
}

func onDemandFor(instanceType string) float64 {
	for _, e := range catalog {
		if e.InstanceType == instanceType {
			return e.OnDemandUSD
		}
	}
	return 0
}

func refState(s *ec2types.InstanceState) compute.RefState {
	if s == nil {
		return compute.RefUnknown
	}
	switch s.Name {
	case ec2types.InstanceStateNamePending:
		return compute.RefPending
	case ec2types.InstanceStateNameRunning:
		return compute.RefRunning
	case ec2types.InstanceStateNameShuttingDown, ec2types.InstanceStateNameStopping:
		return compute.RefStopping
	case ec2types.InstanceStateNameTerminated, ec2types.InstanceStateNameStopped:
		return compute.RefTerminated
	default:
		return compute.RefUnknown
	}
}

// cloudInit renders the bootstrap: runtime bundle → agent binary → systemd
// unit registering with the one-time token. Machines need nothing pre-baked.
func cloudInit(b compute.Bootstrap) string {
	return fmt.Sprintf(`#!/bin/bash
set -euo pipefail

# flint-agent bootstrap (rendered by the awsec2 provider)
mkdir -p /var/lib/flint-agent /etc/flint-agent

# Container runtime bundle (containerd + runc, agent-supervised).
curl -fsSL %[1]s/scripts/bundle-runtime.sh | sh -s /var/lib/flint-agent/bin || true

# Agent binary.
ARCH=$(uname -m); case "$ARCH" in x86_64) ARCH=amd64;; aarch64) ARCH=arm64;; esac
curl -fsSL -o /usr/local/bin/flint-agent "%[2]s/flint-agent-linux-$ARCH"
chmod 0755 /usr/local/bin/flint-agent

cat > /etc/flint-agent/env <<EOF
FLINT_AGENT_SERVER=%[3]s
FLINT_AGENT_TOKEN=%[4]s
FLINT_AGENT_MACHINE_ID=%[5]s
EOF
chmod 0600 /etc/flint-agent/env

cat > /etc/systemd/system/flint-agent.service <<'EOF'
[Unit]
Description=Flint machine agent
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=/etc/flint-agent/env
ExecStart=/usr/local/bin/flint-agent daemon
Restart=always
RestartSec=2
Delegate=yes
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now flint-agent
`, b.ServerHTTPURL, b.AgentDownloadURL, b.ServerGRPCURL, b.RegistrationToken, b.MachineID)
}
