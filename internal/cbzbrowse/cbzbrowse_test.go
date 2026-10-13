package cbzbrowse

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeCBZ builds an in-memory CBZ (zip) with the given file entries.
func makeCBZ(t *testing.T, files map[string][]byte) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		fw, err := zw.Create(name)
		require.NoError(t, err)
		_, err = fw.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	b := buf.Bytes()
	return bytes.NewReader(b)
}

func TestListEntries_Root(t *testing.T) {
	rdr := makeCBZ(t, map[string][]byte{
		"page001.jpg":    []byte("fake-jpg-1"),
		"page002.png":    []byte("fake-png-2"),
		"ComicInfo.xml":  []byte("<ComicInfo/>"),
		"subfolder/a.jpg": []byte("nested"),
	})

	resp, err := ListEntries(rdr, int64(rdr.Len()), "")
	require.NoError(t, err)
	assert.Equal(t, 4, resp.Total) // subfolder dir + 3 files
	// Directories come first.
	assert.True(t, resp.Entries[0].IsDir)
	assert.Equal(t, "subfolder", resp.Entries[0].Name)
}

func TestListEntries_Subfolder(t *testing.T) {
	rdr := makeCBZ(t, map[string][]byte{
		"ch1/page001.jpg": []byte("img1"),
		"ch1/page002.jpg": []byte("img2"),
		"ch2/page001.jpg": []byte("img3"),
	})

	resp, err := ListEntries(rdr, int64(rdr.Len()), "ch1")
	require.NoError(t, err)
	assert.Equal(t, 2, resp.Total)
	for _, e := range resp.Entries {
		assert.False(t, e.IsDir)
	}
}

func TestExtractFile(t *testing.T) {
	rdr := makeCBZ(t, map[string][]byte{
		"page001.jpg": []byte("image-data-here"),
	})

	rec := httptest.NewRecorder()
	err := ExtractFile(rec, rdr, int64(rdr.Len()), "page001.jpg")
	require.NoError(t, err)
	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "image-data-here", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "image/jpeg")
}

func TestExtractFile_NotFound(t *testing.T) {
	rdr := makeCBZ(t, map[string][]byte{
		"page001.jpg": []byte("data"),
	})

	rec := httptest.NewRecorder()
	err := ExtractFile(rec, rdr, int64(rdr.Len()), "nonexistent.jpg")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestGetMetadata_WithComicInfo(t *testing.T) {
	ci := ComicInfo{
		Title:     "Test Comic",
		Series:    "Test Series",
		Number:    "1",
		Writer:    "Author A",
		Publisher: "Test Pub",
		Year:      2024,
		Month:     6,
		Pages: &ComicPages{
			Page: []ComicPageInfo{
				{Image: 0, Type: "FrontCover"},
				{Image: 1, Type: "Story"},
			},
		},
	}
	ciXML, err := xml.Marshal(ci)
	require.NoError(t, err)

	rdr := makeCBZ(t, map[string][]byte{
		"page001.jpg":   []byte("img1"),
		"page002.jpg":   []byte("img2"),
		"page003.png":   []byte("img3"),
		"ComicInfo.xml": ciXML,
	})

	meta, err := GetMetadata(rdr, int64(rdr.Len()))
	require.NoError(t, err)
	require.NotNil(t, meta.ComicInfo)
	assert.Equal(t, "Test Comic", meta.ComicInfo.Title)
	assert.Equal(t, "Test Series", meta.ComicInfo.Series)
	assert.Equal(t, "1", meta.ComicInfo.Number)
	assert.Equal(t, "Author A", meta.ComicInfo.Writer)
	assert.Equal(t, "Test Pub", meta.ComicInfo.Publisher)
	assert.Equal(t, 2024, meta.ComicInfo.Year)
	assert.Equal(t, 3, meta.TotalPages) // 3 image files
	require.Len(t, meta.ComicInfo.Pages.Page, 2)
	assert.Equal(t, "FrontCover", meta.ComicInfo.Pages.Page[0].Type)
}

func TestGetMetadata_WithoutComicInfo(t *testing.T) {
	rdr := makeCBZ(t, map[string][]byte{
		"page001.jpg": []byte("img1"),
		"page002.jpg": []byte("img2"),
	})

	meta, err := GetMetadata(rdr, int64(rdr.Len()))
	require.NoError(t, err)
	assert.Nil(t, meta.ComicInfo)
	assert.Equal(t, 2, meta.TotalPages)
}

func TestGetMetadata_CaseInsensitiveXmlName(t *testing.T) {
	rdr := makeCBZ(t, map[string][]byte{
		"page001.jpg":   []byte("img"),
		"comicinfo.xml": []byte("<ComicInfo><Title>Lowercase</Title></ComicInfo>"),
	})

	meta, err := GetMetadata(rdr, int64(rdr.Len()))
	require.NoError(t, err)
	require.NotNil(t, meta.ComicInfo)
	assert.Equal(t, "Lowercase", meta.ComicInfo.Title)
}

func TestExtractCover(t *testing.T) {
	rdr := makeCBZ(t, map[string][]byte{
		"page002.jpg": []byte("second-image"),
		"page001.jpg": []byte("first-image"),
		"readme.txt":  []byte("not an image"),
	})

	rec := httptest.NewRecorder()
	err := ExtractCover(rec, rdr, int64(rdr.Len()))
	require.NoError(t, err)
	assert.Equal(t, 200, rec.Code)
	assert.Equal(t, "first-image", rec.Body.String())
	assert.Contains(t, rec.Header().Get("Content-Type"), "image/jpeg")
}

func TestExtractCover_NoImages(t *testing.T) {
	rdr := makeCBZ(t, map[string][]byte{
		"readme.txt":    []byte("text"),
		"ComicInfo.xml": []byte("<ComicInfo/>"),
	})

	rec := httptest.NewRecorder()
	err := ExtractCover(rec, rdr, int64(rdr.Len()))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no image files")
}

func TestParseComicInfo_FullSchema(t *testing.T) {
	xmlData := []byte(`<?xml version="1.0" encoding="utf-8"?>
<ComicInfo>
  <Title>My Comic</Title>
  <Series>Epic Series</Series>
  <Number>42</Number>
  <Count>100</Count>
  <Volume>2024</Volume>
  <Summary>A great story</Summary>
  <Notes>Scanned by someone</Notes>
  <AlternateSeries>Crossover</AlternateSeries>
  <AlternateNumber>3</AlternateNumber>
  <AlternateCount>5</AlternateCount>
  <Year>2024</Year>
  <Month>3</Month>
  <Day>15</Day>
  <Publisher>Big Pub</Publisher>
  <Imprint>Sub Pub</Imprint>
  <Format>TPB</Format>
  <Writer>Writer A, Writer B</Writer>
  <Penciller>Artist A</Penciller>
  <Inker>Inker A</Inker>
  <Colorist>Color A</Colorist>
  <Letterer>Letter A</Letterer>
  <CoverArtist>Cover A</CoverArtist>
  <Editor>Editor A</Editor>
  <Translator>Trans A</Translator>
  <Genre>Action, Sci-Fi</Genre>
  <Tags>space, aliens</Tags>
  <Characters>Hero, Villain</Characters>
  <Teams>Team Alpha</Teams>
  <Locations>Earth, Mars</Locations>
  <MainCharacterOrTeam>Hero</MainCharacterOrTeam>
  <AgeRating>Teen</AgeRating>
  <StoryArc>Big Event</StoryArc>
  <StoryArcNumber>2</StoryArcNumber>
  <SeriesGroup>Universe X</SeriesGroup>
  <PageCount>24</PageCount>
  <LanguageISO>en</LanguageISO>
  <Web>https://example.com</Web>
  <BlackAndWhite>No</BlackAndWhite>
  <Manga>No</Manga>
  <GTIN>1234567890123</GTIN>
  <CommunityRating>4.5</CommunityRating>
  <Review>Great read</Review>
  <ScanInformation>Scanned at 300dpi</ScanInformation>
  <Pages>
    <Page Image="0" Type="FrontCover" DoublePage="false" ImageSize="123456" ImageWidth="1280" ImageHeight="1920" />
    <Page Image="1" Type="Story" DoublePage="false" />
    <Page Image="2" Type="Story DoublePage" DoublePage="true" ImageWidth="2560" ImageHeight="1920" />
  </Pages>
</ComicInfo>`)

	ci, err := ParseComicInfo(xmlData)
	require.NoError(t, err)

	assert.Equal(t, "My Comic", ci.Title)
	assert.Equal(t, "Epic Series", ci.Series)
	assert.Equal(t, "42", ci.Number)
	assert.Equal(t, 100, ci.Count)
	assert.Equal(t, "2024", ci.Volume)
	assert.Equal(t, "A great story", ci.Summary)
	assert.Equal(t, "Scanned by someone", ci.Notes)
	assert.Equal(t, "Crossover", ci.AlternateSeries)
	assert.Equal(t, "3", ci.AlternateNumber)
	assert.Equal(t, 5, ci.AlternateCount)
	assert.Equal(t, 2024, ci.Year)
	assert.Equal(t, 3, ci.Month)
	assert.Equal(t, 15, ci.Day)
	assert.Equal(t, "Big Pub", ci.Publisher)
	assert.Equal(t, "Sub Pub", ci.Imprint)
	assert.Equal(t, "TPB", ci.Format)
	assert.Equal(t, "Writer A, Writer B", ci.Writer)
	assert.Equal(t, "Artist A", ci.Penciller)
	assert.Equal(t, "Inker A", ci.Inker)
	assert.Equal(t, "Color A", ci.Colorist)
	assert.Equal(t, "Letter A", ci.Letterer)
	assert.Equal(t, "Cover A", ci.CoverArtist)
	assert.Equal(t, "Editor A", ci.Editor)
	assert.Equal(t, "Trans A", ci.Translator)
	assert.Equal(t, "Action, Sci-Fi", ci.Genre)
	assert.Equal(t, "space, aliens", ci.Tags)
	assert.Equal(t, "Hero, Villain", ci.Characters)
	assert.Equal(t, "Team Alpha", ci.Teams)
	assert.Equal(t, "Earth, Mars", ci.Locations)
	assert.Equal(t, "Hero", ci.MainCharacterOrTeam)
	assert.Equal(t, "Teen", ci.AgeRating)
	assert.Equal(t, "Big Event", ci.StoryArc)
	assert.Equal(t, "2", ci.StoryArcNumber)
	assert.Equal(t, "Universe X", ci.SeriesGroup)
	assert.Equal(t, 24, ci.PageCount)
	assert.Equal(t, "en", ci.LanguageISO)
	assert.Equal(t, "https://example.com", ci.Web)
	assert.Equal(t, "No", ci.BlackAndWhite)
	assert.Equal(t, "No", ci.Manga)
	assert.Equal(t, "1234567890123", ci.GTIN)
	assert.Equal(t, 4.5, ci.CommunityRating)
	assert.Equal(t, "Great read", ci.Review)
	assert.Equal(t, "Scanned at 300dpi", ci.ScanInformation)

	require.NotNil(t, ci.Pages)
	require.Len(t, ci.Pages.Page, 3)

	p0 := ci.Pages.Page[0]
	assert.Equal(t, 0, p0.Image)
	assert.Equal(t, "FrontCover", p0.Type)
	assert.False(t, p0.DoublePage)
	assert.Equal(t, int64(123456), p0.ImageSize)
	assert.Equal(t, 1280, p0.ImageWidth)
	assert.Equal(t, 1920, p0.ImageHeight)

	p2 := ci.Pages.Page[2]
	assert.Equal(t, 2, p2.Image)
	assert.Equal(t, "Story DoublePage", p2.Type) // v2.1 space-separated types
	assert.True(t, p2.DoublePage)
}

func TestParseComicInfo_Invalid(t *testing.T) {
	_, err := ParseComicInfo([]byte("not xml at all"))
	assert.Error(t, err)
}

func TestIsImageExt(t *testing.T) {
	assert.True(t, isImageExt(".jpg"))
	assert.True(t, isImageExt(".JPG"))
	assert.True(t, isImageExt(".png"))
	assert.True(t, isImageExt(".webp"))
	assert.True(t, isImageExt(".avif"))
	assert.False(t, isImageExt(".xml"))
	assert.False(t, isImageExt(".txt"))
	assert.False(t, isImageExt(""))
}

// Silence the unused import warning for io - it's used in the test helpers.
var _ = io.EOF
