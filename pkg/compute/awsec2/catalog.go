package awsec2

// catalogEntry describes one EC2 instance type Flint can quote. Prices are
// us-east-1 on-demand baselines (regional multipliers are close enough for
// ranking; billing truth comes from AWS). Boot seconds are observed medians
// for stock AL2023 with the agent bootstrap. The catalog is deliberately a
// curated common-types list, not the full 700-type zoo — pools narrow further
// with instanceTypes allow-lists.
type catalogEntry struct {
	InstanceType    string
	CPUMillis       int64
	MemoryMB        int64
	Arch            string // amd64 | arm64
	OnDemandUSD     float64
	BootSeconds     int
	InterruptionPct float64 // rough spot reclaim likelihood bucket
}

var catalog = []catalogEntry{
	// t3 — burstable amd64
	{"t3.medium", 2000, 4096, "amd64", 0.0416, 35, 0.05},
	{"t3.large", 2000, 8192, "amd64", 0.0832, 35, 0.05},
	{"t3.xlarge", 4000, 16384, "amd64", 0.1664, 35, 0.05},
	// t4g — burstable arm64
	{"t4g.medium", 2000, 4096, "arm64", 0.0336, 35, 0.05},
	{"t4g.large", 2000, 8192, "arm64", 0.0672, 35, 0.05},
	{"t4g.xlarge", 4000, 16384, "arm64", 0.1344, 35, 0.05},
	// m7i — general purpose amd64
	{"m7i.large", 2000, 8192, "amd64", 0.1008, 40, 0.08},
	{"m7i.xlarge", 4000, 16384, "amd64", 0.2016, 40, 0.08},
	{"m7i.2xlarge", 8000, 32768, "amd64", 0.4032, 40, 0.08},
	{"m7i.4xlarge", 16000, 65536, "amd64", 0.8064, 40, 0.08},
	// m7g — general purpose arm64
	{"m7g.large", 2000, 8192, "arm64", 0.0816, 40, 0.06},
	{"m7g.xlarge", 4000, 16384, "arm64", 0.1632, 40, 0.06},
	{"m7g.2xlarge", 8000, 32768, "arm64", 0.3264, 40, 0.06},
	{"m7g.4xlarge", 16000, 65536, "arm64", 0.6528, 40, 0.06},
	// c7i — compute optimized amd64 (the CI sweet spot)
	{"c7i.large", 2000, 4096, "amd64", 0.08925, 40, 0.10},
	{"c7i.xlarge", 4000, 8192, "amd64", 0.1785, 40, 0.10},
	{"c7i.2xlarge", 8000, 16384, "amd64", 0.357, 40, 0.10},
	{"c7i.4xlarge", 16000, 32768, "amd64", 0.714, 40, 0.10},
	{"c7i.8xlarge", 32000, 65536, "amd64", 1.428, 45, 0.10},
	// c7g — compute optimized arm64
	{"c7g.large", 2000, 4096, "arm64", 0.0725, 40, 0.07},
	{"c7g.xlarge", 4000, 8192, "arm64", 0.145, 40, 0.07},
	{"c7g.2xlarge", 8000, 16384, "arm64", 0.29, 40, 0.07},
	{"c7g.4xlarge", 16000, 32768, "arm64", 0.58, 40, 0.07},
	{"c7g.8xlarge", 32000, 65536, "arm64", 1.16, 45, 0.07},
	// r7i — memory optimized amd64
	{"r7i.large", 2000, 16384, "amd64", 0.1323, 40, 0.08},
	{"r7i.xlarge", 4000, 32768, "amd64", 0.2646, 40, 0.08},
	{"r7i.2xlarge", 8000, 65536, "amd64", 0.5292, 40, 0.08},
	// r7g — memory optimized arm64
	{"r7g.large", 2000, 16384, "arm64", 0.1071, 40, 0.06},
	{"r7g.xlarge", 4000, 32768, "arm64", 0.2142, 40, 0.06},
	{"r7g.2xlarge", 8000, 65536, "arm64", 0.4284, 40, 0.06},
}
