package awsec2

import (
	"context"
	"fmt"
	"sync"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/compute"
	"github.com/NerdMeNot/flint/pkg/compute/computetest"
)

// mockEC2 is an in-memory EC2 control plane for the provider tests.
type mockEC2 struct {
	mu        sync.Mutex
	instances map[string]*mockInstance // instance id → state
	byToken   map[string]string        // client token → instance id (idempotency)
	seq       int
	spotPrice string
}

type mockInstance struct {
	id    string
	state ec2types.InstanceStateName
	tags  map[string]string
}

func newMockEC2() *mockEC2 {
	return &mockEC2{
		instances: map[string]*mockInstance{},
		byToken:   map[string]string{},
		spotPrice: "0.0350",
	}
}

func (m *mockEC2) RunInstances(_ context.Context, in *ec2.RunInstancesInput, _ ...func(*ec2.Options)) (*ec2.RunInstancesOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	token := awssdk.ToString(in.ClientToken)
	if id, ok := m.byToken[token]; ok {
		return runOut(id), nil // idempotent per client token
	}
	m.seq++
	id := fmt.Sprintf("i-%08d", m.seq)
	tags := map[string]string{}
	for _, spec := range in.TagSpecifications {
		for _, tag := range spec.Tags {
			tags[awssdk.ToString(tag.Key)] = awssdk.ToString(tag.Value)
		}
	}
	m.instances[id] = &mockInstance{id: id, state: ec2types.InstanceStateNameRunning, tags: tags}
	m.byToken[token] = id
	return runOut(id), nil
}

func runOut(id string) *ec2.RunInstancesOutput {
	return &ec2.RunInstancesOutput{Instances: []ec2types.Instance{{InstanceId: awssdk.String(id)}}}
}

func (m *mockEC2) TerminateInstances(_ context.Context, in *ec2.TerminateInstancesInput, _ ...func(*ec2.Options)) (*ec2.TerminateInstancesOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range in.InstanceIds {
		if inst, ok := m.instances[id]; ok {
			inst.state = ec2types.InstanceStateNameTerminated
		}
	}
	return &ec2.TerminateInstancesOutput{}, nil
}

func (m *mockEC2) DescribeInstances(_ context.Context, _ *ec2.DescribeInstancesInput, _ ...func(*ec2.Options)) (*ec2.DescribeInstancesOutput, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var instances []ec2types.Instance
	for _, inst := range m.instances {
		if inst.tags["flint:managed"] != "true" {
			continue
		}
		state := inst.state
		instances = append(instances, ec2types.Instance{
			InstanceId: awssdk.String(inst.id),
			State:      &ec2types.InstanceState{Name: state},
		})
	}
	return &ec2.DescribeInstancesOutput{
		Reservations: []ec2types.Reservation{{Instances: instances}},
	}, nil
}

func (m *mockEC2) DescribeSpotPriceHistory(_ context.Context, _ *ec2.DescribeSpotPriceHistoryInput, _ ...func(*ec2.Options)) (*ec2.DescribeSpotPriceHistoryOutput, error) {
	return &ec2.DescribeSpotPriceHistoryOutput{
		SpotPriceHistory: []ec2types.SpotPrice{{SpotPrice: awssdk.String(m.spotPrice)}},
	}, nil
}

func (m *mockEC2) DescribeImages(_ context.Context, _ *ec2.DescribeImagesInput, _ ...func(*ec2.Options)) (*ec2.DescribeImagesOutput, error) {
	return &ec2.DescribeImagesOutput{Images: []ec2types.Image{
		{ImageId: awssdk.String("ami-old"), CreationDate: awssdk.String("2025-01-01T00:00:00Z")},
		{ImageId: awssdk.String("ami-new"), CreationDate: awssdk.String("2026-01-01T00:00:00Z")},
	}}, nil
}

func testProvider() *Provider {
	return newWithAPI("aws-test", Config{
		Region: "us-east-1", SubnetIDs: []string{"subnet-a", "subnet-b"},
		SecurityGroupIDs: []string{"sg-1"},
	}, newMockEC2())
}

// The provider must pass the shared conformance suite over the mock.
func TestEC2_Conformance(t *testing.T) {
	computetest.RunConformance(t, func(t *testing.T) compute.Provider {
		return testProvider()
	})
}

func TestEC2_QuoteSpotAndOnDemand(t *testing.T) {
	p := testProvider()
	offers, err := p.Quote(context.Background(), compute.Requirements{
		CPUMillis: 4000, MemoryMB: 8192, Arch: "arm64", Capacity: compute.CapacityAny,
	})
	require.NoError(t, err)
	require.NotEmpty(t, offers)

	var sawSpot, sawOD bool
	for _, o := range offers {
		assert.Equal(t, "arm64", o.Arch)
		assert.GreaterOrEqual(t, o.CPUMillis, int64(4000))
		switch o.Capacity {
		case compute.CapacitySpot:
			sawSpot = true
			assert.InDelta(t, 0.035, o.PricePerHourUSD, 0.0001, "spot price from the live API")
			assert.Greater(t, o.InterruptionRisk, 0.0)
		case compute.CapacityOnDemand:
			sawOD = true
			assert.Greater(t, o.PricePerHourUSD, 0.0)
		}
	}
	assert.True(t, sawSpot, "capacity=any includes spot")
	assert.True(t, sawOD, "capacity=any includes on-demand")
}

func TestEC2_CreateTagsAndBootstrap(t *testing.T) {
	mock := newMockEC2()
	p := newWithAPI("aws-test", Config{
		Region: "us-east-1", SubnetIDs: []string{"subnet-a"},
	}, mock)

	offers, err := p.Quote(context.Background(), compute.Requirements{
		CPUMillis: 2000, MemoryMB: 4096, Arch: "amd64", Capacity: compute.CapacityOnDemand,
	})
	require.NoError(t, err)
	require.NotEmpty(t, offers)

	ref, err := p.Create(context.Background(), offers[0], compute.Bootstrap{
		MachineID: "m-42", RegistrationToken: "tok", PoolName: "ci",
		ServerGRPCURL: "flint.example:9443", ServerHTTPURL: "https://flint.example",
		AgentDownloadURL: "https://dl.example/agent",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, ref.ID)

	inst := mock.instances[ref.ID]
	require.NotNil(t, inst)
	assert.Equal(t, "true", inst.tags["flint:managed"])
	assert.Equal(t, "m-42", inst.tags["flint:machine-id"])
	assert.Equal(t, "ci", inst.tags["flint:pool"])
}

func TestEC2_CloudInitCarriesBootstrap(t *testing.T) {
	script := cloudInit(compute.Bootstrap{
		ServerGRPCURL: "flint.example:9443", ServerHTTPURL: "https://flint.example",
		MachineID: "m-1", RegistrationToken: "secret-token", PoolName: "ci",
		AgentDownloadURL: "https://dl.example",
	})
	assert.Contains(t, script, "FLINT_AGENT_SERVER=flint.example:9443")
	assert.Contains(t, script, "FLINT_AGENT_TOKEN=secret-token")
	assert.Contains(t, script, "FLINT_AGENT_MACHINE_ID=m-1")
	assert.Contains(t, script, "systemctl enable --now flint-agent")
	assert.Contains(t, script, "bundle-runtime.sh")
}

func TestEC2_ResolveAMIPicksNewest(t *testing.T) {
	p := testProvider()
	ami, err := p.resolveAMI(context.Background(), "amd64")
	require.NoError(t, err)
	assert.Equal(t, "ami-new", ami)
}
