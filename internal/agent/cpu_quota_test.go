package agent

import "testing"

func TestCollectCPUHonorsQuotaAndCpuset(t *testing.T) {
	for _, tc := range []struct {
		name, v2, quota, period, cpuset, want string
	}{
		{name: "v2 integer quota", v2: "200000 100000", want: "2 cores allocated"},
		{name: "v2 fractional quota", v2: "150000 100000", want: "1.5 cores allocated"},
		{name: "v2 subcore quota", v2: "50000 100000", want: "0.5 cores allocated"},
		{name: "cpuset stricter", v2: "300000 100000", cpuset: "0-1", want: "2 cores allocated"},
		{name: "quota stricter", v2: "150000 100000", cpuset: "0-2", want: "1.5 cores allocated"},
		{name: "available processors stricter", v2: "800000 100000", want: "4 cores"},
		{name: "equal available processors", v2: "400000 100000", want: "4 cores"},
		{name: "v1 integer quota", quota: "200000", period: "100000", want: "2 cores allocated"},
		{name: "v1 fractional quota", quota: "125000", period: "100000", want: "1.25 cores allocated"},
		{name: "v1 unlimited", quota: "-1", period: "100000", want: "4 cores"},
		{name: "v2 unlimited wins over legacy files", v2: "max 100000", quota: "100000", period: "100000", want: "4 cores"},
		{name: "v2 unlimited respects cpuset", v2: "max 100000", cpuset: "0", want: "1 cores allocated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := fixtureRoot(t, map[string]string{
				"proc/cpuinfo":                        cpuinfo4Core,
				"sys/fs/cgroup/cpu.max":               tc.v2,
				"sys/fs/cgroup/cpuset.cpus.effective": tc.cpuset,
				"sys/fs/cgroup/cpu/cpu.cfs_quota_us":  tc.quota,
				"sys/fs/cgroup/cpu/cpu.cfs_period_us": tc.period,
			})
			want := "AMD Ryzen 9 5950X 16-Core Processor (" + tc.want + ")"
			if got := CollectCPU(r); got != want {
				t.Fatalf("CollectCPU = %q, want %q", got, want)
			}
		})
	}
}

func TestCollectCPUIgnoresInvalidQuota(t *testing.T) {
	for _, raw := range []string{"", "max", "100000", "100000 0", "0 100000", "-1 100000", "1 -100000", "NaN 100000", "100000 nope", "9223372036854775808 100000", "100000 100000 extra"} {
		t.Run(raw, func(t *testing.T) {
			r := fixtureRoot(t, map[string]string{
				"proc/cpuinfo": cpuinfo4Core, "sys/fs/cgroup/cpu.max": raw,
			})
			if got := CollectCPU(r); got != "AMD Ryzen 9 5950X 16-Core Processor (4 cores)" {
				t.Fatalf("invalid quota changed CPU facts: %q", got)
			}
		})
	}
}

func TestCollectCPUReadsCombinedV1CPUController(t *testing.T) {
	r := fixtureRoot(t, map[string]string{
		"proc/cpuinfo": cpuinfo4Core,
		"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us":  "50000",
		"sys/fs/cgroup/cpu,cpuacct/cpu.cfs_period_us": "100000",
	})
	if got := CollectCPU(r); got != "AMD Ryzen 9 5950X 16-Core Processor (0.5 cores allocated)" {
		t.Fatalf("combined v1 quota ignored: %q", got)
	}
}
