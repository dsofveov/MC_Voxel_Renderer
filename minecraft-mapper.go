package main

import (	"os";
		"flag";
		"runtime";
		"fmt";
		"regexp";
		"strconv";
		"io";
		"io/ioutil";
		"compress/zlib";
		"strings";
		"bytes";
		"math";)

var threads uint
func init() {
	flag.UintVar(&threads, "threads", 8, "Number of threads in which to run")
}
var worldPath = flag.String("worldPath", "", "Path to the world Folder.  Required")
var regionLoaderResultChan = make(chan [2]int)
var regionLoaderSyncChan = make(chan int)
var world [][]chunk
var worldUpdaterChan = make(chan *region)
var worldUpdaterSyncChan = make(chan int)
type chunk struct {
	blocks [16][128][16]uint8
	timeStamp uint32
}
type region struct {
	x, z int
	chunks [32][32]chunk
}
type nbt struct {
	parent *nbt
	tagType uint8
	tagName string
	tagPayload []byte
	tagList []*nbt
	tagListType uint8
	tagCompound []*nbt
}

func main() {
	flag.Parse()
	path := string(*worldPath)
	runtime.GOMAXPROCS(int(threads))
	if path[len(path)-1:] != "/" {
		path += "/"
	}
	path += "region/"
	fileinfo, error := os.Stat(path)
	if error != nil {
		fmt.Println("Error: could not open regions folder (", error, ")")
		os.Exit(1)
	}
	if !fileinfo.IsDirectory() {
		fmt.Println("Error: worldPath did not point to the world directory.")
		os.Exit(1)
	}
	file, error := os.Open(path)
	if error != nil {
		fmt.Println("Error: problem opening region directory (", error, ")")
		os.Exit(1)
	}
	files, error := file.Readdirnames(-1)
	if error != nil {
		fmt.Println("Error: problem reading region directory (", error, ")")
		os.Exit(1)
	}
	for i := 0; i < len(files); i++ {
		go processRegion(path, files[i])
	}
	regionMaxX :=0
	regionMinX := 0
	regionMaxZ := 0
	regionMinZ := 0
	for i := 0; i < len(files); i++ {
		regionExtents := <- regionLoaderResultChan
		if regionExtents[0] > regionMaxX {
			regionMaxX = regionExtents[0]
		}
		if regionExtents[0] < regionMinX {
			regionMinX = regionExtents[0]
		}
		if regionExtents[1] > regionMaxZ {
			regionMaxZ = regionExtents[1]
		}
		if regionExtents[1] < regionMinZ {
			regionMinZ = regionExtents[1]
		}
	}
	go worldUpdater(regionMaxX, regionMinX, regionMaxZ, regionMinZ)
	for i := 0; i < len(files); i++ {
		regionLoaderSyncChan <- 1
	}
	for i := 0; i < len(files); i++ {
		<- worldUpdaterSyncChan
	}
	sliceRender()
}

func sliceRender() {
	var colourTable [256][3]int
	for colour := 0; colour < 256; colour++ {
		colourTable[colour][0] = 63
		colourTable[colour][1] = 63
		colourTable[colour][2] = 63
	}
	colourTable[1] = [3]int{63, 63, 63}
	colourTable[2][1] = 127
	colourTable[3] = [3]int{127, 0, 127}
	colourTable[4] = colourTable[1]
	colourTable[5] = [3]int{63, 31, 0}
	colourTable[8][2] = 127
	colourTable[9][2] = 127
	colourTable[12] = [3]int{127, 127, 0}
	colourTable[18] = [3]int{31, 127, 31}
	colourTable[78] = [3]int{127, 127, 127}
	colourTable[79] = [3]int{95, 95, 127}
	var topHistogram [256]uint64
	var top uint8
	var histogram [256]uint64
	outputFile, error := os.OpenFile("output.bmp", os.O_CREATE | os.O_WRONLY, 0666)
	if error != nil {
		fmt.Println("Error: problem creating the output file")
	}
	header1 := []byte{66, 77, 54, 0, 32, 1, 0, 0, 0, 0, 54, 0, 0, 0, 40, 0, 0}
	xRes := len(world[0]) * 16
	yRes := len(world) * 16
	xResByte := []byte{byte(xRes >> 24), byte(xRes >> 16), byte(xRes >> 8), byte(xRes)}
	yResByte := []byte{byte(yRes >> 24), byte(yRes >> 16), byte(yRes >> 8), byte(yRes)}
	header2 := []byte{0, 1, 0, 24, 0, 0, 0, 0, 0, 0, 0, 32, 1, 19, 11, 0, 0, 19, 11, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	outputFile.Write(header1)
	outputFile.Write(xResByte)
	outputFile.Write(yResByte)
	outputFile.Write(header2)
	for chunkX := len(world) - 1; chunkX >= 0; chunkX-- {
		for x := 15; x >= 0; x-- {
			for chunkY := len(world[0]) - 1; chunkY >= 0; chunkY-- {
				for y := 15; y >= 0; y-- {
					highest := 0
					top = 0
					for height := 127; height >= 0; height-- {
						histogram[world[chunkX][chunkY].blocks[x][height][y]]++
						if world[chunkX][chunkY].blocks[x][height][y] != 0 && world[chunkX][chunkY].blocks[x][height][y] != 50 && highest < height{
							highest = height
							top = world[chunkX][chunkY].blocks[x][height][y]
						}
					}
					topHistogram[top]++
					colourValue := []byte{byte(highest + colourTable[top][2]), byte(highest + colourTable[top][1]), byte(highest + colourTable[top][0])}
					outputFile.Write(colourValue)
				}
			}
		}
	}
	if error = outputFile.Close(); error != nil {
		fmt.Println("Error: problem closing the output file")
	}
//	for i := 0; i < 256; i++ {
//		fmt.Println("Block", i, "=", histogram[i], "(all),", topHistogram[i], "(top)")
//	}
}

func processRegion(path string, filename string) {
	newRegion := new(region)
	regex := regexp.MustCompile(`r\.(-?[0-9]+)\.(-?[0-9]+)\.mcr`)
	regionFile, error := os.Open(path + filename)
	if error != nil {
		fmt.Println("Error: could not read region file '", filename, "' (", error, ")")
		os.Exit(1)
	}
	if !regex.Match([]uint8(filename)) {
		fmt.Println("Warning: skipping unusually-named file '", filename, "'")
	} else {
		matches := regex.FindAllStringSubmatch(filename, 1)
		regionX, err1 := strconv.Atoi(matches[0][1])
		regionZ, err2 := strconv.Atoi(matches[0][2])
		if err1 != nil || err2 != nil {
			fmt.Println("Error: failed to extract coordinates from file '", filename, "'")
			os.Exit(1)
		}
		newRegion.x = regionX
		newRegion.z = regionZ
		regionContents, err := ioutil.ReadAll(io.Reader(regionFile))
		if err != nil {
			fmt.Println("Error: failed to read region header from file '", filename, "'")
			os.Exit(1)
		}
		for chunkX := 0; chunkX < 32; chunkX++ {
			for chunkZ := 0; chunkZ < 32; chunkZ++ {
				offset := 4 * ((chunkX % 32) + ((chunkZ % 32) * 32))
				newRegion.chunks[chunkX][chunkZ].timeStamp = uint32(regionContents[offset + 4096])<<24 | uint32(regionContents[offset + 4097])<<16 | uint32(regionContents[offset + 4098])<<8 | uint32(regionContents[offset + 4099])
				chunkOffset := (uint32(regionContents[offset])<<16 | uint32(regionContents[offset + 1])<< 8 | uint32(regionContents[offset + 2]))<<12
				chunkSize := uint32(regionContents[offset + 3])<<12
				if chunkOffset > 0 && chunkSize > 0 {
					chunkLength := uint32(regionContents[chunkOffset])<<24 | uint32(regionContents[chunkOffset + 1])<<16 | uint32(regionContents[chunkOffset + 2])<<8 | uint32(regionContents[chunkOffset + 3])
					chunkCompression := regionContents[chunkOffset + 4]
					chunkCompressed := regionContents[chunkOffset + 5:chunkOffset + 4 + chunkLength]
					var chunkUncompressed []uint8
					if chunkCompression == 1 {
						fmt.Println("Error: chunk with Gzip compression method detected.")
						os.Exit(1)
					} else if chunkCompression == 2 {
						zlibReader, error := zlib.NewReader(strings.NewReader(string(chunkCompressed)))
						if error != nil {
							fmt.Println("Error attempting to decompress a chunk (", error, ").")
							os.Exit(1)
						}
						output := bytes.NewBuffer(nil)
						_, error = output.ReadFrom(zlibReader)
						if error != nil {
							fmt.Println("Error decompressing a chunk (", error, ").")
							os.Exit(1)
						}
						chunkUncompressed = []uint8(output.String())
						error = zlibReader.Close()
						if error != nil {
							fmt.Println("Error cleaning up after decompressing a chunk (", error, ").")
							os.Exit(1)
						}
					} else {
						fmt.Println("Error: chunk with undefined compression method detected.")
						os.Exit(1)
					}
					nbtChan := make(chan *nbt)
					go nbtReader(chunkUncompressed[:], nbtChan)
					chunkNbt := <- nbtChan
					
					//	Move down the tree to the actual data
					chunkNbt = chunkNbt.tagCompound[0].tagCompound[0]
					
					//	Extract data from the nbt tree and add to our own
					for i := 0; i < len(chunkNbt.tagCompound); i++ {
						thisElement := chunkNbt.tagCompound[i]
						switch thisElement.tagName {
						case "Blocks":
							counter := 0
							for x := 0; x < 16; x++ {
								for z := 0; z < 16; z++ {
									for y := 0; y < 128; y++ {
										newRegion.chunks[chunkX][chunkZ].blocks[x][y][z] = thisElement.tagPayload[counter]
										counter++
									}
								}
							}
						}
					}
				//	if regionX == 0 && regionZ == 0 && chunkX == 0 && chunkZ == 0 {
				//		printNbt(chunkNbt, "")
				//	}
				}
			}
		}
	}
	if regionFile.Close() != nil {
		fmt.Println("Error: problem closing region file '", filename, "' (", error, ")")
		os.Exit(1)
	}
	regionLoaderResultChan <- [2]int{newRegion.x, newRegion.z}
	<- regionLoaderSyncChan
	worldUpdaterChan <- newRegion
}

func worldUpdater(maxX, minX, maxZ, minZ int) {
	world = make([][]chunk, (maxX - minX + 1) * 32)
	for i := 0; i < len(world); i++ {
		world[i] = make([]chunk, (maxZ - minZ + 1) * 32)
	}
	for {
		region := <- worldUpdaterChan
		regionX := (region.x - minX) * 32
		regionZ := (region.z - minZ) * 32
		for x := 0; x < 32; x++ {
			for z := 0; z < 32; z++ {
				world[x + regionX][z + regionZ] = region.chunks[x][z]
			}
		}
		worldUpdaterSyncChan <- 1
	}
}

func printNbt(input *nbt, level string) {
	name := input.tagName
	if name == "" {
		name = "No Name"
	}
	fmt.Println(level + "- " + name + " ( type", input.tagType, ")")
	var payload string
	var payloadValue uint64
	if input.tagType < 7 && input.tagType > 0 {
		payloadValue = 0
		for i := 0; i < len(input.tagPayload); i++ {
			payloadValue = payloadValue<<8 | uint64(input.tagPayload[i])
		}
	}
	switch input.tagType {
	case 1, 2, 3, 4: payload = fmt.Sprintf("%d", payloadValue)
	case 5: payload = fmt.Sprintf("%e", math.Float32frombits(uint32(payloadValue)))
	case 6: payload = fmt.Sprintf("%e", math.Float64frombits(payloadValue))
	case 7: payload = "Array of length " + strconv.Itoa(len(input.tagPayload)) + " bytes"
	case 8: payload = string(input.tagPayload)
	}
	if input.tagType > 0 && input.tagType < 9 {
		fmt.Println(level + "    +Payload =", payload)
	}
	if input.tagListType != 0 {
		fmt.Println(level + "    |-List ( type", input.tagListType, ")")
		for i := 0; i < len(input.tagList); i++ {
			if i < len(input.tagList) - 1 || i == 1 {
				printNbt(input.tagList[i], level + "    |")
			} else {
				printNbt(input.tagList[i], level + "     ")
			}
		}
	}
	if len(input.tagCompound) > 0 {
		fmt.Println(level + " +Compound")
		for i := 0; i < len(input.tagCompound); i++ {
			if i < len(input.tagCompound) - 1 || i == 1 {
				printNbt(input.tagCompound[i], level + "    |")
			} else {
				printNbt(input.tagCompound[i], level + "     ")
			}
		}
	}
}

func nbtReader(rawData []byte, output chan *nbt) {
	rootNbt := new(nbt)
	rootNbt.parent = rootNbt
	parentNbt := rootNbt
	rootNbt.tagType = 10
	rootNbt.tagName = "Root node"
	var thisNbt *nbt
	position := uint64(0)
	for position < uint64(len(rawData)) {
		thisNbt = new(nbt)
		thisNbt.parent = parentNbt
		var listPosition uint32
		if parentNbt.tagType == 9 {
			listPosition = 0
			done := false
			for done == false {
				if parentNbt.tagList[listPosition] != nil {
					listPosition++
				} else {
					done = true
				}
			}
			thisNbt.tagType = parentNbt.tagListType
		} else if parentNbt.tagType == 10 {
			thisNbt.tagType = rawData[position]
			position++
			if thisNbt.tagType != 0 {
				nameLength := uint64(uint16(rawData[position])<<8 | uint16(rawData[position + 1]))
				position += 2
				thisNbt.tagName = string(rawData[position:position + nameLength])
				position += nameLength
			}
		} else {
			fmt.Println("Error: trying to add child records to a non-supporting NBT record type.")
			os.Exit(1)
		}
		if thisNbt.tagType == 0 {
			if parentNbt.tagType == 10 {
				parentNbt = parentNbt.parent
				for parentNbt.tagType == 9 && parentNbt.tagList[len(parentNbt.tagList) - 1] != nil {
					parentNbt = parentNbt.parent
				}
			} else {
				fmt.Println("Error: Unexpected end tag")
				os.Exit(1)
			}
		} else if thisNbt.tagType == 1 {
			thisNbt.tagPayload = rawData[position:position + 1]
			position += 1
			if parentNbt.tagType == 10 {
				parentNbt.tagCompound = append(parentNbt.tagCompound, thisNbt)
			} else if parentNbt.tagType == 9 {
				parentNbt.tagList[listPosition] = thisNbt
				for parentNbt.tagType == 9 && parentNbt.tagList[len(parentNbt.tagList) - 1] != nil {
					parentNbt = parentNbt.parent
				}
			}
		} else if thisNbt.tagType == 2 {
			thisNbt.tagPayload = rawData[position:position + 2]
			position += 2
			if parentNbt.tagType == 10 {
				parentNbt.tagCompound = append(parentNbt.tagCompound, thisNbt)
			} else if parentNbt.tagType == 9 {
				parentNbt.tagList[listPosition] = thisNbt
				for parentNbt.tagType == 9 && parentNbt.tagList[len(parentNbt.tagList) - 1] != nil {
					parentNbt = parentNbt.parent
				}
			}
		} else if thisNbt.tagType == 3 {
			thisNbt.tagPayload = rawData[position:position + 4]
			position += 4
			if parentNbt.tagType == 10 {
				parentNbt.tagCompound = append(parentNbt.tagCompound, thisNbt)
			} else if parentNbt.tagType == 9 {
				parentNbt.tagList[listPosition] = thisNbt
				for parentNbt.tagType == 9 && parentNbt.tagList[len(parentNbt.tagList) - 1] != nil {
					parentNbt = parentNbt.parent
				}
			}
		} else if thisNbt.tagType == 4 {
			thisNbt.tagPayload = rawData[position:position + 8]
			position += 8
			if parentNbt.tagType == 10 {
				parentNbt.tagCompound = append(parentNbt.tagCompound, thisNbt)
			} else if parentNbt.tagType == 9 {
				parentNbt.tagList[listPosition] = thisNbt
				for parentNbt.tagType == 9 && parentNbt.tagList[len(parentNbt.tagList) - 1] != nil {
					parentNbt = parentNbt.parent
				}
			}

		} else if thisNbt.tagType == 5 {
			thisNbt.tagPayload = rawData[position:position + 4]
			position += 4
			if parentNbt.tagType == 10 {
				parentNbt.tagCompound = append(parentNbt.tagCompound, thisNbt)
			} else if parentNbt.tagType == 9 {
				parentNbt.tagList[listPosition] = thisNbt
				for parentNbt.tagType == 9 && parentNbt.tagList[len(parentNbt.tagList) - 1] != nil {
					parentNbt = parentNbt.parent
				}
			}
		} else if thisNbt.tagType == 6 {
			thisNbt.tagPayload = rawData[position:position + 8]
			position += 8
			if parentNbt.tagType == 10 {
				parentNbt.tagCompound = append(parentNbt.tagCompound, thisNbt)
			} else if parentNbt.tagType == 9 {
				parentNbt.tagList[listPosition] = thisNbt
				for parentNbt.tagType == 9 && parentNbt.tagList[len(parentNbt.tagList) - 1] != nil {
					parentNbt = parentNbt.parent
				}
			}
		} else if thisNbt.tagType == 7 {
			arrayLength := uint64(uint32(rawData[position])<<24 | uint32(rawData[position + 1])<<16 | uint32(rawData[position + 2])<<8 | uint32(rawData[position + 3]))
			position += 4
			thisNbt.tagPayload = rawData[position:position + arrayLength]
			position += arrayLength
			if parentNbt.tagType == 10 {
				parentNbt.tagCompound = append(parentNbt.tagCompound, thisNbt)
			} else if parentNbt.tagType == 9 {
				parentNbt.tagList[listPosition] = thisNbt
				for parentNbt.tagType == 9 && parentNbt.tagList[len(parentNbt.tagList) - 1] != nil {
					parentNbt = parentNbt.parent
				}
			}
		} else if thisNbt.tagType == 8 {
			payloadLength := uint64(uint16(rawData[position])<<8 | uint16(rawData[position + 1]))
			position += 2
			thisNbt.tagPayload = rawData[position:position + payloadLength]
			position += payloadLength
			if parentNbt.tagType == 10 {
				parentNbt.tagCompound = append(parentNbt.tagCompound, thisNbt)
			} else if parentNbt.tagType == 9 {
				parentNbt.tagList[listPosition] = thisNbt
				for parentNbt.tagType == 9 && parentNbt.tagList[len(parentNbt.tagList) - 1] != nil {
					parentNbt = parentNbt.parent
				}
			}
		} else if thisNbt.tagType == 9 {
			thisNbt.tagListType = rawData[position]
			position++
			arrayLength := uint32(rawData[position])<<24 | uint32(rawData[position + 1])<<16 | uint32(rawData[position + 2])<<8 | uint32(rawData[position + 3])
			position += 4
			thisNbt.tagList = make([]*nbt, arrayLength)
			if parentNbt.tagType == 10 {
				parentNbt.tagCompound = append(parentNbt.tagCompound, thisNbt)
			} else if parentNbt.tagType == 9 {
				parentNbt.tagList[listPosition] = thisNbt
			}
			if arrayLength > 0 {
				parentNbt = thisNbt
			}
		} else if thisNbt.tagType == 10 {
			if parentNbt.tagType == 10 {
				parentNbt.tagCompound = append(parentNbt.tagCompound, thisNbt)
			} else if parentNbt.tagType == 9 {
				parentNbt.tagList[listPosition] = thisNbt
			}
			parentNbt = thisNbt
		} else {
			fmt.Println("Error: Unknown tag")
			os.Exit(1)
		}
	}
	output <- rootNbt
}
