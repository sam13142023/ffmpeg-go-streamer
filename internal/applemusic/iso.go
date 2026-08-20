package applemusic

// iso3to2 maps the ISO-3166 alpha-3 country codes iTunes returns to the alpha-2
// storefront codes wrapper-manager / amp-api expect. Only common storefronts are
// listed; unknown codes fall back to the configured storefront.
var iso3to2 = map[string]string{
	"USA": "us", "GBR": "gb", "CAN": "ca", "AUS": "au", "DEU": "de",
	"FRA": "fr", "JPN": "jp", "KOR": "kr", "CHN": "cn", "TWN": "tw",
	"HKG": "hk", "SGP": "sg", "IND": "in", "BRA": "br", "MEX": "mx",
	"ESP": "es", "ITA": "it", "NLD": "nl", "SWE": "se", "NOR": "no",
	"DNK": "dk", "FIN": "fi", "RUS": "ru", "POL": "pl", "TUR": "tr",
	"IDN": "id", "THA": "th", "VNM": "vn", "PHL": "ph", "MYS": "my",
	"NZL": "nz", "IRL": "ie", "PRT": "pt", "CHE": "ch", "AUT": "at",
	"BEL": "be", "ZAF": "za", "ARE": "ae", "SAU": "sa", "ARG": "ar",
	"CHL": "cl", "COL": "co", "UKR": "ua", "GRC": "gr", "CZE": "cz",
}
