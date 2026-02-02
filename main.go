package main

import (
	"fmt"
	"math/rand"
	"time"
)

const (
	W = 44
	H = 44
)

// ===== TYPES =====

type Faction int

const (
	EMPTY Faction = iota
	BLUE
	RED
	GREEN
	MAGENTA
)

type Cell struct {
	faction  Faction
	energy   int
	resource int
}

type Agent struct {
	x, y    int
	faction Faction
	symbol  string
}

var grid [H][W]Cell
var agents []Agent

// ===== VISUALS =====

var colors = map[Faction]string{
	EMPTY:   "\x1b[37m",
	BLUE:    "\x1b[34m",
	RED:     "\x1b[31m",
	GREEN:   "\x1b[32m",
	MAGENTA: "\x1b[35m",
}

var symbols = map[Faction][]string{
	BLUE:    {"❖", "◆"},
	RED:     {"●", "◉"},
	GREEN:   {"■", "▲"},
	MAGENTA: {"✖", "✦"},
}

// ===== INIT =====

func initGrid() {
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			grid[y][x] = Cell{
				faction:  EMPTY,
				energy:   rand.Intn(5),
				resource: rand.Intn(10),
			}
		}
	}
}

func seedAgents() {
	factions := []Faction{BLUE, RED, GREEN, MAGENTA}

	for i := 0; i < 120; i++ {
		f := factions[rand.Intn(len(factions))]
		syms := symbols[f]

		agents = append(agents, Agent{
			x:       rand.Intn(W),
			y:       rand.Intn(H),
			faction: f,
			symbol:  syms[rand.Intn(len(syms))],
		})
	}
}

// ===== HELPERS =====

func clamp(v, min, max int) int {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func neighbors(x, y int) []Cell {
	var out []Cell
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if dx == 0 && dy == 0 {
				continue
			}
			nx, ny := x+dx, y+dy
			if nx >= 0 && nx < W && ny >= 0 && ny < H {
				out = append(out, grid[ny][nx])
			}
		}
	}
	return out
}

// ===== SIMULATION =====

func updateAgents() {
	for i := range agents {
		a := &agents[i]

		// random drift + slight attraction to own faction
		dx := rand.Intn(3) - 1
		dy := rand.Intn(3) - 1

		n := neighbors(a.x, a.y)

		for _, c := range n {
			if c.faction == a.faction {
				if rand.Float64() < 0.3 {
					dx += rand.Intn(3) - 1
					dy += rand.Intn(3) - 1
				}
			}
		}

		a.x = clamp(a.x+dx, 0, W-1)
		a.y = clamp(a.y+dy, 0, H-1)

		cell := &grid[a.y][a.x]

		if cell.faction == EMPTY || cell.energy < 2 {
			cell.faction = a.faction
			cell.energy = 5
		} else if cell.faction != a.faction {

			// resource war
			if rand.Intn(10)+a.factionPower() > cell.energy {
				cell.faction = a.faction
				cell.energy = 5
			}
		}

		cell.resource--
		if cell.resource < 0 {
			cell.resource = rand.Intn(10)
		}
	}
}

func (a Agent) factionPower() int {
	switch a.faction {
	case BLUE:
		return 3
	case RED:
		return 4
	case GREEN:
		return 2
	case MAGENTA:
		return 6
	}
	return 1
}

func updateCells() {
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			c := &grid[y][x]

			if c.faction == EMPTY {
				continue
			}

			n := neighbors(x, y)

			same := 0
			diff := 0

			for _, nb := range n {
				if nb.faction == c.faction {
					same++
				} else if nb.faction != EMPTY {
					diff++
				}
			}

			// stabilize
			if same >= 3 {
				c.energy++
			}

			// decay under pressure
			if diff >= 4 {
				c.energy -= 2
			}

			if c.energy <= 0 {
				c.faction = EMPTY
				c.energy = 0
			}

			// chaos injection (prevents crystal freeze)
			if rand.Float64() < 0.0008 {
				c.faction = Faction(rand.Intn(4) + 1)
				c.energy = 4
			}
		}
	}
}

// ===== RENDER =====

func clear() {
	fmt.Print("\x1b[2J\x1b[H")
}

func draw() {
	for y := 0; y < H; y++ {
		for x := 0; x < W; x++ {
			c := grid[y][x]

			if c.faction == EMPTY {
				fmt.Print("≈")
				continue
			}

			syms := symbols[c.faction]
			ch := syms[rand.Intn(len(syms))]

			fmt.Printf("%s%s", colors[c.faction], ch)
		}
		fmt.Print("\x1b[0m\n")
	}
}

// ===== MAIN =====

func main() {
	rand.Seed(time.Now().UnixNano())

	initGrid()
	seedAgents()

	for {
		clear()

		updateAgents()
		updateCells()

		draw()

		time.Sleep(60 * time.Millisecond)
	}
}
