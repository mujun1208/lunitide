package officestudio

type PerfSample struct {
	Fixture, Kind, Reason string
	ElapsedMS             int64
	Skipped               bool
}

type PerfSummary struct {
	Ready bool
	P50MS int64
	N     int
}

func MeasureOfficeFixtures(n int) []PerfSample {
	if n < 30 {
		return []PerfSample{{Skipped: true, Reason: "need ≥30 runs on a fixed machine; not a product P50"}}
	}
	return nil
}

func SummarizePerf(samples []PerfSample) PerfSummary {
	for _, s := range samples {
		if s.Skipped {
			return PerfSummary{Ready: false}
		}
	}
	return PerfSummary{Ready: false, N: len(samples)}
}
