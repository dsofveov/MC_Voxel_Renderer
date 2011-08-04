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
		"bytes";)

var threads uint
func init() {
	flag.UintVar(&threads, "threads", 8, "Number of threads in which to run")
}
var worldPath = flag.String("worldPath", "/var/lib/minecraft/1.7/world", "Path to the world Folder.  Required")
type nbt struct {
	parent *nbt
	tagType uint8
	tagName string
	tagPayload []byte
	tagList []*nbt
	tagListType uint8
	tagCompound []*nbt
}
var decoderSyncChan = make(chan int)

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
	for i := 0; i < len(files); i++ {
		<- decoderSyncChan
	}
	fmt.Println("Done!")
	for {
	}
}

func processRegion(path string, filename string) {
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
//		regionX
		_, err1 := strconv.Atoi(matches[0][1])
//		regionZ
		_, err2 := strconv.Atoi(matches[0][2])
		if err1 != nil || err2 != nil {
			fmt.Println("Error: failed to extract coordinates from file '", filename, "'")
			os.Exit(1)
		}
		regionContents, err := ioutil.ReadAll(io.Reader(regionFile))
		if err != nil {
			fmt.Println("Error: failed to read region header from file '", filename, "'")
			os.Exit(1)
		}
		for chunkX := 0; chunkX < 32; chunkX++ {
			for chunkZ := 0; chunkZ < 32; chunkZ++ {
				offset := 4 * ((chunkX % 32) + ((chunkZ % 32) * 32))
//				timeStamp := uint32(regionContents[offset + 4096])<<24 | uint32(regionContents[offset + 4097])<<16 | uint32(regionContents[offset + 4098])<<8 | uint32(regionContents[offset + 4099])
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
//										newRegion.chunks[chunkX][chunkZ].blocks[x][y][z] = thisElement.tagPayload[counter]
										counter++
									}
								}
							}
						}
					}
				}
			}
		}
	}
	if regionFile.Close() != nil {
		fmt.Println("Error: problem closing region file '", filename, "' (", error, ")")
		os.Exit(1)
	}
	decoderSyncChan <- 1
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
