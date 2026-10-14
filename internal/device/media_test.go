package device

import (
	"testing"

	"github.com/TechBlueprints/disunified/internal/devicemodel"
)

func TestInformMediaVocabulary(t *testing.T) {
	for _, tc := range []struct {
		media devicemodel.Media
		want  string
	}{
		{devicemodel.MediaCopper1G, "GE"},
		{devicemodel.MediaCopper2G5, "2.5GE"},
		{devicemodel.MediaCopper10G, "10GE"},
		{devicemodel.MediaSFP, "SFP"},
		{devicemodel.MediaSFPPlus, "SFP+"},
		{devicemodel.MediaSFP28, "SFP28"},
		{devicemodel.MediaQSFPPlus, "QSFP28"},
		{devicemodel.MediaQSFP28, "QSFP28"},
	} {
		if got := unifiMedia(tc.media); got != tc.want {
			t.Errorf("media %s = %q, want %q", tc.media, got, tc.want)
		}
	}
}
