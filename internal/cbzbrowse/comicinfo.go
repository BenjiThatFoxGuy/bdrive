// Package cbzbrowse provides server-side browsing of CBZ (Comic Book Archive)
// files and parsing of embedded ComicInfo.xml metadata per the Anansi Project
// schema (https://anansi-project.github.io/docs/comicinfo/documentation).
package cbzbrowse

import "encoding/xml"

// ComicInfo represents the metadata stored in a ComicInfo.xml file inside
// a CBZ archive.  Fields follow the Anansi Project / ComicRack schema.
type ComicInfo struct {
	XMLName xml.Name `xml:"ComicInfo" json:"-"`

	// Book identification
	Title    string `xml:"Title,omitempty"    json:"title,omitempty"`
	Series   string `xml:"Series,omitempty"   json:"series,omitempty"`
	Number   string `xml:"Number,omitempty"   json:"number,omitempty"`
	Count    int    `xml:"Count,omitempty"    json:"count,omitempty"`
	Volume   string `xml:"Volume,omitempty"   json:"volume,omitempty"`
	Summary  string `xml:"Summary,omitempty"  json:"summary,omitempty"`
	Notes    string `xml:"Notes,omitempty"    json:"notes,omitempty"`

	// Alternate series (cross-over / event tracking)
	AlternateSeries  string `xml:"AlternateSeries,omitempty"  json:"alternateSeries,omitempty"`
	AlternateNumber  string `xml:"AlternateNumber,omitempty"  json:"alternateNumber,omitempty"`
	AlternateCount   int    `xml:"AlternateCount,omitempty"   json:"alternateCount,omitempty"`

	// Publication dates
	Year  int `xml:"Year,omitempty"  json:"year,omitempty"`
	Month int `xml:"Month,omitempty" json:"month,omitempty"`
	Day   int `xml:"Day,omitempty"   json:"day,omitempty"`

	// Publisher info
	Publisher string `xml:"Publisher,omitempty" json:"publisher,omitempty"`
	Imprint   string `xml:"Imprint,omitempty"   json:"imprint,omitempty"`
	Format    string `xml:"Format,omitempty"    json:"format,omitempty"`

	// Creators (comma-separated when multiple)
	Writer      string `xml:"Writer,omitempty"      json:"writer,omitempty"`
	Penciller   string `xml:"Penciller,omitempty"   json:"penciller,omitempty"`
	Inker       string `xml:"Inker,omitempty"       json:"inker,omitempty"`
	Colorist    string `xml:"Colorist,omitempty"    json:"colorist,omitempty"`
	Letterer    string `xml:"Letterer,omitempty"    json:"letterer,omitempty"`
	CoverArtist string `xml:"CoverArtist,omitempty" json:"coverArtist,omitempty"`
	Editor      string `xml:"Editor,omitempty"      json:"editor,omitempty"`
	Translator  string `xml:"Translator,omitempty"  json:"translator,omitempty"`

	// Content classification
	Genre                string `xml:"Genre,omitempty"                json:"genre,omitempty"`
	Tags                 string `xml:"Tags,omitempty"                 json:"tags,omitempty"`
	Characters           string `xml:"Characters,omitempty"           json:"characters,omitempty"`
	Teams                string `xml:"Teams,omitempty"                json:"teams,omitempty"`
	Locations            string `xml:"Locations,omitempty"            json:"locations,omitempty"`
	MainCharacterOrTeam  string `xml:"MainCharacterOrTeam,omitempty"  json:"mainCharacterOrTeam,omitempty"`
	AgeRating            string `xml:"AgeRating,omitempty"            json:"ageRating,omitempty"`

	// Story arc / organization
	StoryArc       string `xml:"StoryArc,omitempty"       json:"storyArc,omitempty"`
	StoryArcNumber string `xml:"StoryArcNumber,omitempty" json:"storyArcNumber,omitempty"`
	SeriesGroup    string `xml:"SeriesGroup,omitempty"    json:"seriesGroup,omitempty"`

	// Technical / descriptive
	PageCount       int     `xml:"PageCount,omitempty"       json:"pageCount,omitempty"`
	LanguageISO     string  `xml:"LanguageISO,omitempty"     json:"languageISO,omitempty"`
	Web             string  `xml:"Web,omitempty"             json:"web,omitempty"`
	BlackAndWhite   string  `xml:"BlackAndWhite,omitempty"   json:"blackAndWhite,omitempty"`
	Manga           string  `xml:"Manga,omitempty"           json:"manga,omitempty"`
	GTIN            string  `xml:"GTIN,omitempty"            json:"gtin,omitempty"`
	CommunityRating float64 `xml:"CommunityRating,omitempty" json:"communityRating,omitempty"`
	Review          string  `xml:"Review,omitempty"          json:"review,omitempty"`

	// Scan info
	ScanInformation string `xml:"ScanInformation,omitempty" json:"scanInformation,omitempty"`

	// Page metadata
	Pages *ComicPages `xml:"Pages,omitempty" json:"pages,omitempty"`
}

// ComicPages wraps the list of page entries.
type ComicPages struct {
	Page []ComicPageInfo `xml:"Page" json:"page,omitempty"`
}

// ComicPageInfo describes a single page inside the comic.
type ComicPageInfo struct {
	Image       int    `xml:"Image,attr"                  json:"image"`
	Type        string `xml:"Type,attr,omitempty"          json:"type,omitempty"`
	DoublePage  bool   `xml:"DoublePage,attr,omitempty"    json:"doublePage,omitempty"`
	ImageSize   int64  `xml:"ImageSize,attr,omitempty"     json:"imageSize,omitempty"`
	ImageWidth  int    `xml:"ImageWidth,attr,omitempty"    json:"imageWidth,omitempty"`
	ImageHeight int    `xml:"ImageHeight,attr,omitempty"   json:"imageHeight,omitempty"`
	Bookmark    string `xml:"Bookmark,attr,omitempty"      json:"bookmark,omitempty"`
	Key         string `xml:"Key,attr,omitempty"           json:"key,omitempty"`
}

// ParseComicInfo unmarshals a ComicInfo.xml byte slice.
func ParseComicInfo(data []byte) (*ComicInfo, error) {
	var ci ComicInfo
	if err := xml.Unmarshal(data, &ci); err != nil {
		return nil, err
	}
	return &ci, nil
}
