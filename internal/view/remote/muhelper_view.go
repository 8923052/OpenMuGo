package remote

// muhelper_view.go —— MU Helper 出站，对应 OpenMU RemoteView/MuHelper/
// MuHelperConfigurationUpdatePlugIn（C2 AE 回显程序）与 MuHelperStatusUpdatePlugIn
// （C1 BF 51：Start=(false,0,false)、Stop=(false,0,true)、ConsumeMoney=(true,money,false)）。

import (
	s2c "mugo/internal/proto/s2c"
)

func (v *PlayerView) ShowMuHelperConfiguration(data []byte) error {
	p := s2c.NewMuHelperConfigurationData()
	copy(p.HelperData(), data)
	return v.send.Send(p.Bytes())
}

func (v *PlayerView) ShowMuHelperStatus(consumeMoney bool, money uint32, paused bool) error {
	p := s2c.NewMuHelperStatusUpdate()
	p.SetConsumeMoney(consumeMoney)
	p.SetMoney(money)
	p.SetPauseStatus(paused)
	return v.send.Send(p.Bytes())
}
