package bgg

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const sampleSearchXML = `<?xml version="1.0" encoding="utf-8"?>
<items total="2" termsofuse="https://boardgamegeek.com/xmlapi/termsofuse">
	<item type="boardgame" id="167791">
		<name type="primary" value="Terraforming Mars"/>
		<yearpublished value="2016"/>
	</item>
	<item type="boardgameexpansion" id="248591">
		<name type="primary" value="Dinosaur Island: Totally Liquid"/>
		<yearpublished value="2018"/>
	</item>
</items>`

const sampleThingXML = `<?xml version="1.0" encoding="utf-8"?>
<items total="1" termsofuse="https://boardgamegeek.com/xmlapi/termsofuse">
	<item type="boardgame" id="167791">
		<thumbnail>https://cf.geekdo-images.com/thumb.jpg</thumbnail>
		<image>https://cf.geekdo-images.com/full.jpg</image>
		<name type="primary" sortindex="1" value="Terraforming Mars" />
		<description>In the 2400s, mankind begins to terraform Mars...</description>
		<yearpublished value="2016" />
		<minplayers value="1" />
		<maxplayers value="5" />
		<poll name="suggested_numplayers" title="User Suggested Number of Players" totalvotes="1200">
			<results numplayers="1">
				<result value="Best" numvotes="150" />
				<result value="Recommended" numvotes="400" />
			</results>
			<results numplayers="3">
				<result value="Best" numvotes="850" />
				<result value="Recommended" numvotes="250" />
			</results>
		</poll>
		<statistics page="1">
			<ratings>
				<average value="8.38" />
				<averageweight value="3.26" />
			</ratings>
		</statistics>
	</item>
</items>`

func TestSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(sampleSearchXML))
	}))
	defer server.Close()

	client := NewClient("dummy-token")
	client.BaseURL = server.URL

	items, err := client.Search("mars")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("Expected 2 items, got %d", len(items))
	}
	if items[0].ID != 167791 || items[0].Name != "Terraforming Mars" || items[0].YearPublished != 2016 {
		t.Errorf("Unexpected first item: %+v", items[0])
	}
	if items[1].ID != 248591 || items[1].Type != "boardgameexpansion" {
		t.Errorf("Unexpected second item: %+v", items[1])
	}
}

func TestGetThingDetails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.Write([]byte(sampleThingXML))
	}))
	defer server.Close()

	client := NewClient("")
	client.BaseURL = server.URL

	details, err := client.GetThingDetails(167791)
	if err != nil {
		t.Fatalf("GetThingDetails failed: %v", err)
	}
	if details.Name != "Terraforming Mars" {
		t.Errorf("Expected 'Terraforming Mars', got %s", details.Name)
	}
	if details.MinPlayers != 1 || details.MaxPlayers != 5 {
		t.Errorf("Expected 1-5 players, got %d-%d", details.MinPlayers, details.MaxPlayers)
	}
	if details.BestPlayers != "3" {
		t.Errorf("Expected best players '3', got '%s'", details.BestPlayers)
	}
	if details.Rating != 8.38 || details.Complexity != 3.26 {
		t.Errorf("Unexpected rating/complexity: %f / %f", details.Rating, details.Complexity)
	}
}

func TestExtractBggIDFromURL(t *testing.T) {
	tests := []struct {
		input    string
		expected int
		valid    bool
	}{
		{"167791", 167791, true},
		{"https://boardgamegeek.com/boardgame/167791/terraforming-mars", 167791, true},
		{"https://boardgamegeek.com/boardgameexpansion/248591/dinosaur-island-totally-liquid", 248591, true},
		{"https://boardgamegeek.com/boardgame/342942", 342942, true},
		{"invalid-url", 0, false},
		{"", 0, false},
	}

	for _, tt := range tests {
		id, ok := ExtractBggIDFromURL(tt.input)
		if ok != tt.valid || id != tt.expected {
			t.Errorf("ExtractBggIDFromURL(%q) = (%d, %v), expected (%d, %v)", tt.input, id, ok, tt.expected, tt.valid)
		}
	}
}
