package d2s

import "fmt"

// huffmanNode is a node in the Huffman binary tree used to decode
// D2R item type codes. Leaf nodes have a non-zero symbol.
type huffmanNode struct {
	left   *huffmanNode // bit 0
	right  *huffmanNode // bit 1
	symbol byte         // ASCII character (only set on leaves)
}

func (n *huffmanNode) isLeaf() bool {
	return n.left == nil && n.right == nil
}

// huffmanTree decodes Huffman-encoded item type codes in D2R (version >= 0x61).
// The encoding uses 38 symbols (a-z, 0-9, space, NUL) with hardcoded bit patterns
// derived from d07riv's D2R reverse engineering.
type huffmanTree struct {
	root *huffmanNode
}

// d2rHuffmanTable maps each character to its Huffman bit pattern.
// '1' means traverse right, '0' means traverse left.
// Source: dschu012/D2SLib (MIT), originally from d07riv.
var d2rHuffmanTable = map[byte]string{
	' ': "10",
	'0': "11111011",
	'1': "1111100",
	'2': "001100",
	'3': "1101101",
	'4': "11111010",
	'5': "00010110",
	'6': "1101111",
	'7': "01111",
	'8': "000100",
	'9': "01110",
	'a': "11110",
	'b': "0101",
	'c': "01000",
	'd': "110001",
	'e': "110000",
	'f': "010011",
	'g': "11010",
	'h': "00011",
	'i': "1111110",
	'j': "000101110",
	'k': "010010",
	'l': "11101",
	'm': "01101",
	'n': "001101",
	'o': "1111111",
	'p': "11001",
	'q': "11011001",
	'r': "11100",
	's': "0010",
	't': "01100",
	'u': "00001",
	'v': "1101110",
	'w': "00000",
	'x': "00111",
	'y': "0001010",
	'z': "11011000",
}

// newHuffmanTree builds the Huffman tree from the hardcoded table.
func newHuffmanTree() *huffmanTree {
	root := &huffmanNode{}
	for sym, bits := range d2rHuffmanTable {
		cur := root
		for _, b := range bits {
			if b == '1' {
				if cur.right == nil {
					cur.right = &huffmanNode{}
				}
				cur = cur.right
			} else {
				if cur.left == nil {
					cur.left = &huffmanNode{}
				}
				cur = cur.left
			}
		}
		cur.symbol = sym
	}
	return &huffmanTree{root: root}
}

// decodeChar reads bits from br until a leaf node is reached, returning the symbol.
func (ht *huffmanTree) decodeChar(br *bitReader) (byte, error) {
	cur := ht.root
	for !cur.isLeaf() {
		bit, err := br.ReadBit()
		if err != nil {
			return 0, err
		}
		if bit == 1 {
			cur = cur.right
		} else {
			cur = cur.left
		}
		if cur == nil {
			return 0, fmt.Errorf("huffman: invalid bit sequence")
		}
	}
	return cur.symbol, nil
}

// Singleton tree, built once.
var itemCodeTree = newHuffmanTree()
