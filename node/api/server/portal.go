package server

import (
	"github.com/mike76-dev/sia-satellite/modules"
	"github.com/mike76-dev/sia-satellite/node/api"
	"go.sia.tech/jape"
)

func (s *server) portalCreditsHandler(jc jape.Context) {
	jc.Encode(s.p.GetCredits())
}

func (s *server) portalSetCreditsHandler(jc jape.Context) {
	var params modules.CreditData
	if jc.Decode(&params) != nil {
		return
	}

	s.p.SetCredits(params)
}

func (s *server) portalAnnouncementHandler(jc jape.Context) {
	text, expires, err := s.p.GetAnnouncement()
	if jc.Check("failed to read announcement", err) != nil {
		return
	}

	jc.Encode(api.Announcement{
		Text:    text,
		Expires: expires,
	})
}

func (s *server) portalSetAnnouncementHandler(jc jape.Context) {
	var params api.Announcement
	if jc.Decode(&params) != nil {
		return
	}

	if jc.Check("failed to set announcement", s.p.SetAnnouncement(params.Text, params.Expires)) != nil {
		return
	}
}
