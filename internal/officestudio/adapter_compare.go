package officestudio

type AdapterCompareReport struct {
	Presenton, PptxGenJS ExternalAdapterStatus
	SameQualityGate      bool
	Notice               string
}

func CompareExternalAdapters() AdapterCompareReport {
	return AdapterCompareReport{
		Presenton:       ProbePresenton(),
		PptxGenJS:       ProbePptxGenJS(),
		SameQualityGate: true,
		Notice:          "外部生成器只做适配器对照，未进入生产主链；缺进程时检查为 missing，不能绕过交付门槛。",
	}
}
