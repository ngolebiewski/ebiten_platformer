package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	_ "image/png"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	_ "embed"
)

// Embed the assets.
//
//go:embed assets/monochrome_tilemap_transparent_packed.png
var tilesheetBytes []byte

//go:embed assets/tilemap.json
var tilemapJSON []byte

// TiledMap represents the JSON map exported from Tiled.
type TiledMap struct {
	Height     int     `json:"height"`
	Width      int     `json:"width"`
	Tilewidth  int     `json:"tilewidth"`
	Tileheight int     `json:"tileheight"`
	Layers     []Layer `json:"layers"`
}

// Layer represents a layer in the Tiled JSON.
type Layer struct {
	Name   string `json:"name"`
	Data   []int  `json:"data"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Type   string `json:"type"`
}

const (
	screenWidth  = 160
	screenHeight = 160
	tileSize     = 16 // assuming each tile in the tilesheet is 16x16 pixels
)

var (
	tilesImage   *ebiten.Image
	tilemap      TiledMap
	isFullscreen bool // Tracks fullscreen state
)

// Player holds the player's position, size, and velocity.
type Player struct {
	x, y          float64
	vx, vy        float64
	width, height float64
	onGround      bool
}

// collides checks whether the player's bounding box at (newX, newY)
// would intersect any solid tile in the collision layer.
func (p *Player) collides(newX, newY float64, collision *Layer) bool {
	// Determine the tiles covered by the player's new bounding box.
	leftTile := int(newX) / tileSize
	rightTile := int(newX+p.width) / tileSize
	topTile := int(newY) / tileSize
	bottomTile := int(newY+p.height) / tileSize

	for ty := topTile; ty <= bottomTile; ty++ {
		for tx := leftTile; tx <= rightTile; tx++ {
			// Skip out-of-bound indices.
			if tx < 0 || ty < 0 || tx >= collision.Width || ty >= collision.Height {
				continue
			}
			tileIndex := ty*collision.Width + tx
			tile := collision.Data[tileIndex]
			if tile != 0 {
				// Colliding with a solid tile.
				return true
			}
		}
	}
	return false
}

// Move updates the player's position while checking for collisions.
// It applies horizontal and vertical movement separately.
func (p *Player) Move(collision *Layer) {
	// Try horizontal movement.
	newX := p.x + p.vx
	if collision != nil && p.collides(newX, p.y, collision) {
		// Horizontal collision: cancel horizontal velocity.
		p.vx = 0
	} else {
		p.x = newX
	}

	// Try vertical movement.
	newY := p.y + p.vy
	if collision != nil && p.collides(p.x, newY, collision) {
		// Vertical collision: cancel vertical velocity.
		// If moving downward, we assume the player hit the ground.
		if p.vy > 0 {
			p.onGround = true
		}
		p.vy = 0
	} else {
		p.y = newY
		p.onGround = false
	}
}

// Update handles input and physics for the player.
func (p *Player) Update(collision *Layer) {
	// Constants for movement.
	const speed = 2.0
	const jumpSpeed = -5.0
	const gravity = 0.3

	// Horizontal movement.
	if ebiten.IsKeyPressed(ebiten.KeyLeft) {
		p.vx = -speed
	} else if ebiten.IsKeyPressed(ebiten.KeyRight) {
		p.vx = speed
	} else {
		p.vx = 0
	}

	// Jump if on ground.
	if p.onGround && inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		p.vy = jumpSpeed
		p.onGround = false
	}

	// Apply gravity.
	p.vy += gravity

	// Move the player while handling collisions.
	p.Move(collision)
}

// getCollisionLayer searches for a layer named "Collision" and returns it.
func getCollisionLayer(layers []Layer) *Layer {
	for i := range layers {
		if layers[i].Name == "Collision" {
			return &layers[i]
		}
	}
	return nil
}

// Game holds the overall game state.
type Game struct {
	player Player
}

func init() {
	// Decode the embedded tilesheet image.
	img, _, err := image.Decode(bytes.NewReader(tilesheetBytes))
	if err != nil {
		log.Fatal(err)
	}
	tilesImage = ebiten.NewImageFromImage(img)

	// Decode the embedded JSON tilemap.
	if err := json.Unmarshal(tilemapJSON, &tilemap); err != nil {
		log.Fatal(err)
	}
}

func (g *Game) Update() error {
	// Toggle fullscreen when "F" is just pressed.
	if inpututil.IsKeyJustPressed(ebiten.KeyF) {
		isFullscreen = !isFullscreen
		ebiten.SetFullscreen(isFullscreen)
	}

	// Get the collision layer (if available).
	collisionLayer := getCollisionLayer(tilemap.Layers)
	// Update the player with collision checking.
	g.player.Update(collisionLayer)
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	// Fill the background with black.
	screen.Fill(image.Black)

	// Draw the tilemap background (assume the first layer is the visible background).
	if len(tilemap.Layers) > 0 {
		bgLayer := tilemap.Layers[0]
		tilesheetWidth := tilesImage.Bounds().Dx()
		tileXCount := tilesheetWidth / tileSize

		for i, tile := range bgLayer.Data {
			// Tiled exports tile indices starting at 1 (0 means empty), so adjust.
			if tile == 0 {
				continue
			}
			tile-- // convert to 0-based index

			x := i % bgLayer.Width
			y := i / bgLayer.Width

			op := &ebiten.DrawImageOptions{}
			op.GeoM.Translate(float64(x*tileSize), float64(y*tileSize))

			sx := (tile % tileXCount) * tileSize
			sy := (tile / tileXCount) * tileSize
			subImage := tilesImage.SubImage(
				image.Rect(sx, sy, sx+tileSize, sy+tileSize),
			).(*ebiten.Image)
			screen.DrawImage(subImage, op)
		}
	}

	// Draw the player.
	// For now, use sprite index 280 for the idle frame.
	const playerSpriteIndex = 280
	tilesheetWidth := tilesImage.Bounds().Dx()
	tileXCount := tilesheetWidth / tileSize
	sx := (playerSpriteIndex % tileXCount) * tileSize
	sy := (playerSpriteIndex / tileXCount) * tileSize
	playerImage := tilesImage.SubImage(
		image.Rect(sx, sy, sx+tileSize, sy+tileSize),
	).(*ebiten.Image)

	op := &ebiten.DrawImageOptions{}
	// Translate to the player's position.
	op.GeoM.Translate(g.player.x, g.player.y)
	// Set the color matrix so that white (1,1,1,1) becomes bright blue (0,0,1,1).
	op.ColorScale.Scale(0, 0, 1, 1)
	screen.DrawImage(playerImage, op)
	ebitenutil.DebugPrint(screen, fmt.Sprintf("TPS: %0.2f", ebiten.ActualTPS()))
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}

func main() {
	// Initialize the player starting position and size.
	game := &Game{
		player: Player{
			x:        50,
			y:        100,
			width:    tileSize,
			height:   tileSize,
			onGround: false,
		},
	}

	ebiten.SetWindowSize(screenWidth*2, screenHeight*2)
	ebiten.SetWindowTitle("Player with Collision")
	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}
