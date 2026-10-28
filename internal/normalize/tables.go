package normalize

// diacritics maps accented runes to ASCII. Both the correct comma-below
// (ș ț) and the legacy cedilla (ş ţ) forms of Romanian letters are covered.
var diacritics = map[rune]string{
	'ă': "a", 'Ă': "a", 'â': "a", 'Â': "a", 'î': "i", 'Î': "i",
	'ș': "s", 'Ș': "s", 'ş': "s", 'Ş': "s",
	'ț': "t", 'Ț': "t", 'ţ': "t", 'Ţ': "t",
	'á': "a", 'à': "a", 'ä': "a", 'å': "a", 'ã': "a", 'Á': "a", 'À': "a", 'Ä': "a",
	'é': "e", 'è': "e", 'ê': "e", 'ë': "e", 'É': "e", 'È': "e", 'Ê': "e", 'Ë': "e",
	'í': "i", 'ì': "i", 'ï': "i", 'Í': "i", 'Ï': "i",
	'ó': "o", 'ò': "o", 'ô': "o", 'ö': "o", 'ő': "o", 'õ': "o", 'Ó': "o", 'Ö': "o", 'Ő': "o",
	'ú': "u", 'ù': "u", 'û': "u", 'ü': "u", 'ű': "u", 'Ú': "u", 'Ü': "u", 'Ű': "u",
	'ç': "c", 'Ç': "c", 'ñ': "n", 'Ñ': "n", 'ß': "ss",
	'’': "", '‘': "", '´': "", '`': "",
}

// streetTypes maps every spelling of a street type (already folded) to its
// canonical form.
var streetTypes = map[string]string{
	"strada": "strada", "str": "strada", "st": "strada", "strad": "strada",
	"bulevardul": "bulevardul", "bulevard": "bulevardul", "bd": "bulevardul", "bdul": "bulevardul",
	"bld": "bulevardul", "blvd": "bulevardul", "bv": "bulevardul",
	"calea": "calea", "cale": "calea", "cal": "calea",
	"aleea": "aleea", "alee": "aleea", "al": "aleea",
	"soseaua": "soseaua", "sosea": "soseaua", "sos": "soseaua", "sose": "soseaua",
	"piata": "piata", "piat": "piata", "pta": "piata", "pt": "piata",
	"piateta": "piateta",
	"splaiul": "splaiul", "splai": "splaiul", "spl": "splaiul",
	"intrarea": "intrarea", "intrare": "intrarea", "intr": "intrarea", "int": "intrarea",
	"drumul": "drumul", "drum": "drumul",
	"fundatura": "fundatura", "fundaturii": "fundatura", "fund": "fundatura", "fdt": "fundatura",
	"fundacul": "fundacul", "fundac": "fundacul",
	"prelungirea": "prelungirea", "prelungire": "prelungirea", "prel": "prelungirea",
	"stradela": "stradela", "str-la": "stradela", "strla": "stradela",
	"pasajul": "pasajul", "pasaj": "pasajul",
	"ulita": "ulita", "trecatoarea": "trecatoarea", "trecatoare": "trecatoarea",
	"parcul": "parcul", "parc": "parcul", "colonia": "colonia", "colonie": "colonia",
	"sirul": "sirul", "sir": "sirul", "scuarul": "scuarul", "scuar": "scuarul",
	"vadul": "vadul", "vad": "vadul", "podul": "podul", "pod": "podul",
	"hotarul": "hotarul", "hotar": "hotarul", "pietonal": "pietonal", "statia": "statia",
	"varianta": "varianta", "esplanada": "esplanada", "canton": "canton", "canal": "canal",
	"poligon": "poligon", "campul": "campul", "camp": "campul", "poteca": "poteca",
	"suisul": "suisul", "suis": "suisul", "curtea": "curtea", "curte": "curtea",
	"rampa": "rampa", "cartierul": "cartierul", "cartier": "cartierul", "cart": "cartierul",
	"platoul": "platoul", "platou": "platoul",
}

// bigrams re-joins abbreviations that contain a hyphen or dot.
var bigrams = map[string]string{
	"b dul": "bdul", "b d": "bd", "bd ul": "bdul", "p ta": "pta",
	"str la": "strla", "g ral": "general", "c dor": "comandor", "lt col": "locotenent",
	"sf ta": "sfanta",
}

const (
	markerNumber = iota + 1
	markerBlock
	markerSkip
)

var markers = map[string]int{
	"nr": markerNumber, "numar": markerNumber, "numarul": markerNumber, "no": markerNumber,
	"bl": markerBlock, "bloc": markerBlock, "blocul": markerBlock,
	"sc": markerSkip, "scara": markerSkip, "et": markerSkip, "etaj": markerSkip, "etajul": markerSkip,
	"ap": markerSkip, "apt": markerSkip, "apartament": markerSkip, "apartamentul": markerSkip,
	"cam": markerSkip, "camera": markerSkip, "cp": markerSkip, "corp": markerSkip,
}

// expansions turns common abbreviations in street names into full words.
var expansions = map[string]string{
	"sf": "sfantul", "sfant": "sfantul", "sfintii": "sfintii",
	"viteazu": "viteazul",
}

// titles are honorifics that the dataset appends after the name
// ("Grigorescu Eremia, general") and users often prefix ("Str. General Eremia Grigorescu").
var titles = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range []string{
		"general", "gen", "gral", "doctor", "dr", "pictor", "sold", "soldat", "serg", "sergent",
		"maj", "major", "martir", "poet", "prof", "profesor", "lt", "locotenent", "slt", "sublocotenent",
		"ing", "inginer", "cap", "caporal", "cpt", "capitan", "maresal", "scriitor", "comp", "compozitor",
		"erou", "mr", "maior", "av", "aviator", "episcop", "arh", "arhitect", "mitropolit", "colonel", "col",
		"plt", "plutonier", "actor", "academician", "acad", "ziarist", "preot", "cronicar", "sculptor",
		"spatar", "spatarul", "elev", "frt", "fruntas", "avocat", "protopop", "cardinal", "medic", "amiral",
		"dramaturg", "arhimandrit", "ist", "istoric", "mat", "matematician", "fizician", "patriarh",
		"regizor", "comandor", "brig", "sociolog", "filolog", "astronom", "artist", "tenor", "publicist",
		"mester", "vornic", "stolnic", "jr", "post", "mortem", "comisar", "doc", "ministru", "pr",
	} {
		m[w] = true
	}
	return m
}()

var localityPrefixes = map[string]bool{
	"municipiul": true, "mun": true, "municipiu": true, "orasul": true, "oras": true, "or": true,
	"comuna": true, "com": true, "satul": true, "sat": true, "localitatea": true, "loc": true,
}

var countyPrefixes = map[string]bool{"judetul": true, "judet": true, "jud": true, "jd": true}

var localityAliases = map[string]string{
	"buc": "bucuresti", "bucharest": "bucuresti", "bucuresti": "bucuresti", "b": "bucuresti",
	"municipiulbucuresti": "bucuresti",
}

// countyCodes maps the two-letter county codes (license plates / ISO 3166-2:RO)
// and other aliases to county keys.
var countyCodes = map[string]string{
	"ab": "alba", "ar": "arad", "ag": "arges", "bc": "bacau", "bh": "bihor",
	"bn": "bistritanasaud", "bt": "botosani", "bv": "brasov", "br": "braila",
	"b": "bucuresti", "buc": "bucuresti", "bucharest": "bucuresti", "bz": "buzau",
	"cs": "carasseverin", "cl": "calarasi", "cj": "cluj", "ct": "constanta", "cv": "covasna",
	"db": "dambovita", "dj": "dolj", "gl": "galati", "gr": "giurgiu", "gj": "gorj",
	"hr": "harghita", "hd": "hunedoara", "il": "ialomita", "is": "iasi", "if": "ilfov",
	"mm": "maramures", "mh": "mehedinti", "ms": "mures", "nt": "neamt", "ot": "olt",
	"ph": "prahova", "sm": "satumare", "sj": "salaj", "sb": "sibiu", "sv": "suceava",
	"tr": "teleorman", "tm": "timis", "tl": "tulcea", "vs": "vaslui", "vl": "valcea", "vn": "vrancea",
}
