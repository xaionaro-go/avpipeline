package types

type BitRateInfo struct {
	Video Ubps
	Audio Ubps
	Other Ubps
}

type BitRates struct {
	Input   BitRateInfo
	Encoded BitRateInfo
	Output  BitRateInfo
}
