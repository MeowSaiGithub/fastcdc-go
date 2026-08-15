package fastcdc

// gearTable is a deterministic 256-entry table used by the rolling hash.
//
// The table is generated with SplitMix64 using:
//   - seed increment 0x9e3779b97f4a7c15 (64-bit golden-ratio constant)
//   - mixing multipliers 0xbf58476d1ce4e5b9 and 0x94d049bb133111eb
//
// These constants are standard SplitMix64 parameters chosen for strong bit
// diffusion and reproducible pseudo-random output without external state.
var gearTable = buildGearTable()

func buildGearTable() [256]uint64 {
	var table [256]uint64
	var state uint64 = 0x9e3779b97f4a7c15

	for i := range table {
		state += 0x9e3779b97f4a7c15
		z := state
		z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
		z = (z ^ (z >> 27)) * 0x94d049bb133111eb
		z ^= z >> 31
		if z == 0 {
			z = 1
		}
		table[i] = z
	}

	return table
}
