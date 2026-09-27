package macprefs

import (
	"strings"
	"testing"
)

const dockRead = `(
        {
        GUID = 1;
        "tile-data" =         {
            "file-data" =             {
                "_CFURLString" = "file:///Applications/Safari.app/";
                "_CFURLStringType" = 15;
            };
        };
        "tile-type" = "file-tile";
    },
        {
        "tile-data" =         {
            "file-data" =             {
                "_CFURLString" = "file:///Applications/Visual%20Studio%20Code.app/";
                "_CFURLStringType" = 15;
            };
        };
    },
        {
        "tile-data" =         {
            "file-data" =             {
                "_CFURLString" = "file:///Users/dev/Downloads/";
            };
        };
    }
)`

func TestParseDockApps(t *testing.T) {
	got := ParseDockApps(dockRead)
	want := "/Applications/Safari.app,/Applications/Visual Studio Code.app"
	if strings.Join(got, ",") != want {
		t.Errorf("ParseDockApps = %v, want %s (folders are not apps)", got, want)
	}
}

func TestDockTileEscapes(t *testing.T) {
	tile := DockTile("/Applications/R&D Tool.app")
	if !strings.Contains(tile, "file:///Applications/R&amp;D%20Tool.app/") {
		t.Errorf("tile does not escape the path: %s", tile)
	}
	if !strings.HasPrefix(tile, "<dict><key>tile-data</key>") {
		t.Errorf("tile shape: %s", tile)
	}
}
