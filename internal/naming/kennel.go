package naming

import "math/rand/v2"

// Kennel is the word list a generated name draws its memorable half from.
//
// Cocker spaniels, as cocker spaniels are actually named. The mark is a cocker
// spaniel doing zoomies, so the fleet is a kennel. Every word is one segment of
// lowercase letters -- nothing to sanitise away -- and short enough that the
// shape half of a name survives the length budget alongside it.
//
// It is the same list the pool wizard offers, in web/src/lib/pools/names.ts.
// TestKennelMatchesTheWizard fails if the two drift apart, because a fleet
// whose pools are named from one list and whose runners are named from another
// looks like two products.
var Kennel = []string{
	"banjo",
	"biscuit",
	"boogie",
	"bramble",
	"bubbles",
	"cocoa",
	"crumpet",
	"custard",
	"digby",
	"disco",
	"flapjack",
	"gizmo",
	"hazel",
	"jellybean",
	"jitterbug",
	"maple",
	"marmalade",
	"muffin",
	"noodle",
	"pancake",
	"pepper",
	"pickles",
	"popcorn",
	"rascal",
	"rocket",
	"rusty",
	"scampi",
	"toffee",
	"truffle",
	"waffles",
	"wiggles",
	"ziggy",
}

// KennelWord returns one word from the kennel.
//
// The randomness is cosmetic and math/rand is the right amount of machinery
// for it: nothing depends on a word being unguessable. What a name depends on
// for uniqueness is the token beside it, which comes from crypto/rand.
func KennelWord() string { return Kennel[rand.IntN(len(Kennel))] }
