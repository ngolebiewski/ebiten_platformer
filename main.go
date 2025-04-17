package main

import (
	"bytes"
	"encoding/json"
	"image"
	_ "image/png"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
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

var ladderTiles = map[int]string{
	62:  "top",
	82:  "middle",
	122: "bottom",
}

const (
	screenWidth           = 160
	screenHeight          = 160
	tileSize              = 16  // tiles in the tilesheet are 16x16 pixels - 16bit SNES style!!
	ladderCenterThreshold = 5.0 // +/- pixels for being considered in the center
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
	onLadder      bool
	isJumping     bool // Add this field to track jumping state
}

// checkLadder checks if the player is currently overlapping with a ladder tile.
// It returns true if the player is on a ladder and the type of ladder tile ("top", "middle", or "bottom"), otherwise false and "".
// checkLadder checks if the player's horizontal center is within the center
// of a ladder tile.
func (p *Player) checkLadder(ladderLayer *Layer) (bool, string) {
	playerCenterX := p.x + p.width/2
	leftTile := int(p.x) / tileSize
	rightTile := int(p.x+p.width) / tileSize
	bottomTile := int(p.y+p.height) / tileSize
	topTile := int(p.y) / tileSize

	// Helper function to check if player's horizontal center is within a tile's center
	isInLadderCenter := func(tileX int) bool {
		tileCenterX := float64(tileX*tileSize) + float64(tileSize)/2
		isCenter := playerCenterX >= tileCenterX-ladderCenterThreshold && playerCenterX <= tileCenterX+ladderCenterThreshold
		log.Printf("checkCenter - playerCenterX: %.2f, tileX: %d, tileCenterX: %.2f, threshold: %.2f, isCenter: %v", playerCenterX, tileX, tileX, tileCenterX, ladderCenterThreshold, isCenter)
		return isCenter
	}

	// Check bottom edge for entering
	for tx := leftTile; tx <= rightTile; tx++ {
		for ty := bottomTile; ty <= bottomTile; ty++ {
			log.Printf("checkLadder (entry) - tx: %d, ty: %d, playerY: %.2f, bottomTileY: %d", tx, ty, p.y, ty*tileSize)
			if tx < 0 || ty < 0 || tx >= ladderLayer.Width || ty >= ladderLayer.Height {
				continue
			}
			tileIndex := ty*ladderLayer.Width + tx
			if tileIndex < 0 || tileIndex >= len(ladderLayer.Data) { // check for valid tileIndex
				log.Printf("checkLadder (entry) - tileIndex out of bounds: %d, len(ladderLayer.Data): %d", tileIndex, len(ladderLayer.Data))
				continue
			}
			tile := ladderLayer.Data[tileIndex]
			if ladderType, ok := ladderTiles[tile]; ok && isInLadderCenter(tx) {
				log.Printf("checkLadder (entry) - Found ladder tile: %d (%s) at (%d, %d)", tile, ladderType, tx, ty)
				return true, ladderType
			}
		}
	}

	// Check all overlapping tiles if already on ladder
	if p.onLadder {
		for ty := topTile; ty <= bottomTile; ty++ {
			for tx := leftTile; tx <= rightTile; tx++ {
				log.Printf("checkLadder (onLadder) - tx: %d, ty: %d, playerY: %.2f, tileY: %d", tx, ty, p.y, ty*tileSize)
				if tx < 0 || ty < 0 || tx >= ladderLayer.Width || ty >= ladderLayer.Height {
					continue
				}
				tileIndex := ty*ladderLayer.Width + tx
				if tileIndex < 0 || tileIndex >= len(ladderLayer.Data) { // check for valid tileIndex
					log.Printf("checkLadder (onLadder) - tileIndex out of bounds: %d, len(ladderLayer.Data): %d", tileIndex, len(ladderLayer.Data))
					continue
				}
				tile := ladderLayer.Data[tileIndex]
				if ladderType, ok := ladderTiles[tile]; ok && isInLadderCenter(tx) {
					log.Printf("checkLadder (onLadder) - Found ladder tile: %d (%s) at (%d, %d)", tile, ladderType, tx, ty)
					return true, ladderType
				}
			}
		}
	}

	return false, ""
}

// collides checks whether the player's bounding box at (newX, newY)
// would intersect any solid tile in the collision layer.
func (p *Player) collides(newX, newY float64, collision *Layer) bool {
	if collision == nil {
		return false
	}
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
			if tileIndex < 0 || tileIndex >= len(collision.Data) { // Check for valid index
				log.Printf("collides - tileIndex out of bounds: %d, len(collision.Data): %d", tileIndex, len(collision.Data))
				return false // IMPORTANT:  Return false to prevent a crash.  No collision if index is bad.
			}
			tile := collision.Data[tileIndex]
			if tile != 0 {
				// Colliding with a solid tile.
				if p.vy <= 0 && newY < float64(ty*tileSize+tileSize) && newY+p.height > float64(ty*tileSize) {
					// Allow entering platform from below
					if p.vy < 0 {
						return false
					}
				}
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
		// Clamp horizontal position to stay within screen bounds
		p.x = newX
		if p.x < 0 {
			p.x = 0
		}
		if p.x+p.width > screenWidth {
			p.x = screenWidth - p.width
		}
	}
	// Try vertical movement.
	newY := p.y + p.vy
	if collision != nil && p.collides(p.x, newY, collision) {
		// Vertical collision: cancel vertical velocity.
		// If moving downward, we assume the player hit the ground.
		if p.vy > 0 {
			p.onGround = true
			p.isJumping = false // Reset jumping state when landing
		}
		p.vy = 0
	} else {
		p.y = newY
		if p.vy > 0 {
			p.onGround = false // No longer on the ground if moving down
		}
	}
}

// Update handles input and physics for the player.
func (p *Player) Update(collision *Layer, ladderLayer *Layer) {
	// Constants for movement.
	const speed = 1.5
	const jumpSpeed = -5.0
	const gravity = 0.3

	isOnLadder, ladderType := p.checkLadder(ladderLayer)

	log.Printf("Update - Start: onLadder: %v, isOnLadder: %v, ladderType: %s, p.x: %.2f, p.y: %.2f, p.vx: %.2f, p.vy: %.2f",
		p.onLadder, isOnLadder, ladderType, p.x, p.y, p.vx, p.vy)

	// Handle transition off ladder when at the top
	if p.onLadder && ladderType == "top" && p.vy < 0 {
		// Check if player's top edge is above the top of the tile
		playerTopY := p.y
		tileTopY := float64(int(p.y+p.height)/tileSize) * tileSize
		if playerTopY <= tileTopY {
			p.onLadder = false
			p.onGround = false
			log.Println("Update - Exiting ladder at the top")
		}
	}

	// Transitioning onto a ladder
	if !p.onLadder && isOnLadder {
		if p.vy >= 0 {
			p.onLadder = true
			p.vy = 0
			log.Println("Update - Transitioned onto ladder (downward/standing)")
		} else if ebiten.IsKeyPressed(ebiten.KeyUp) {
			p.onLadder = true
			p.vy = -speed
			p.onGround = false
			log.Println("Update - Transitioned onto ladder (pressing up)")
		}
	}

	// Jumping off the ladder
	if p.onLadder && inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		p.onLadder = false
		p.vy = jumpSpeed
		p.onGround = false
		p.isJumping = true
		log.Println("Update - Jumped off ladder")
	}

	// Handle horizontal movement
	if ebiten.IsKeyPressed(ebiten.KeyLeft) {
		p.vx = -speed
		// If on ladder and moving horizontally, transition off
		if p.onLadder {
			p.onLadder = false
			p.onGround = false
			log.Println("Update - left off ladder")
		}
	} else if ebiten.IsKeyPressed(ebiten.KeyRight) {
		p.vx = speed
		// If on ladder and moving horizontally, transition off
		if p.onLadder {
			p.onLadder = false
			p.onGround = false
			log.Println("Update - right off ladder")
		}
	} else {
		p.vx = 0
	}

	// Vertical Ladder movement
	if p.onLadder {
		if ebiten.IsKeyPressed(ebiten.KeyUp) {
			p.vy = -speed
			p.onGround = false
			log.Println("Update - Climbing ladder up")
		} else if ebiten.IsKeyPressed(ebiten.KeyDown) {
			p.vy = speed
			p.onGround = false
			log.Println("Update - Climbing ladder down")
		} else {
			p.vy = 0
			log.Println("Update - On ladder, no vertical input")
		}
	} else {
		// Apply gravity if not on ladder
		p.vy += gravity
		log.Printf("Update - Applying gravity, p.vy: %.2f", p.vy)
	}

	// Regular jump
	if p.onGround && !p.onLadder && inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		p.vy = jumpSpeed
		p.onGround = false
		p.isJumping = true
		log.Println("Update - Regular jump")
	}

	// Move the player
	p.Move(collision)

	// Leaving a ladder
	if p.onLadder && !isOnLadder {
		p.onLadder = false
		log.Println("Update - Left ladder (not overlapping anymore)")
	}

	// Prevent going below ground on ladder
	if p.onLadder && ladderLayer != nil && len(ladderLayer.Data) > 0 {
		bottomLadderY := float64(ladderLayer.Height*tileSize) - tileSize
		if p.y+p.height > bottomLadderY+tileSize {
			p.y = bottomLadderY + tileSize - p.height
			p.vy = 0
			log.Printf("Update - Prevented going below bottom of ladder at Y: %.2f", bottomLadderY)
		}
	}

	// Prevent going off the top of the screen
	if p.y < 0 {
		p.y = 0
		if p.vy < 0 {
			p.vy = 0
			log.Println("Update - Prevented going off top of screen")
		}
	}
	log.Printf("Update - End: onLadder: %v, p.x: %.2f, p.y: %.2f, p.vx: %.2f, p.vy: %.2f",
		p.onLadder, p.x, p.y, p.vx, p.vy)
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

// getLadderLayer searches for a layer named "Ladders" and returns it.
func getLadderLayer(layers []Layer) *Layer {
	for i := range layers {
		if layers[i].Name == "Ladders" {
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
	// Get the ladder layer (if available).
	ladderLayer := getLadderLayer(tilemap.Layers)

	// Update the player with collision and ladder checking.
	if ladderLayer != nil {
		g.player.Update(collisionLayer, ladderLayer)
	} else {
		g.player.Update(collisionLayer, nil) // Pass nil if no ladder layer
	}
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
	const playerSpriteIndex = 280
	tilesheetWidth := tilesImage.Bounds().Dx()
	tileXCount := tilesheetWidth / tileSize
	sx := (playerSpriteIndex % tileXCount) * tileSize
	sy := (playerSpriteIndex / tileXCount) * tileSize
	playerImage := tilesImage.SubImage(
		image.Rect(sx, sy, sx+tileSize, sy+tileSize),
	).(*ebiten.Image)

	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(g.player.x, g.player.y)
	op.ColorScale.Scale(1, 0, 0, 1)
	screen.DrawImage(playerImage, op)
	// ebitenutil.DebugPrint(screen, fmt.Sprintf("TPS: %0.2f", ebiten.ActualTPS()))
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}

func main() {
	// Initialize the player starting position and size.
	game := &Game{
		player: Player{
			x:         10,
			y:         100,
			width:     tileSize,
			height:    tileSize,
			onGround:  false,
			onLadder:  false,
			isJumping: false, // Initialize isJumping to false
		},
	}

	ebiten.SetWindowSize(screenWidth*2, screenHeight*2)
	ebiten.SetWindowTitle("Player with Collision and Ladders")
	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}
