/**

common serialize/deserialize functions

 */
package serializer

import (
	"encoding/binary"
	"fmt"
	"bufio"
)

// writes the field length, then the field to the writer
func WriteFieldBytes(buf *bufio.Writer, bytes []byte) error {
	//write field length
	size := uint32(len(bytes))
	if err := binary.Write(buf, binary.LittleEndian, &size); err != nil {
		return err
	}
	// write field
	n, err := buf.Write(bytes);
	if err != nil {
		return err
	}
	if uint32(n) != size {
		return fmt.Errorf("unexpected num bytes written. Expected %v, got %v", size, n)
	}
	return nil
}

// read field bytes
func ReadFieldBytes(buf *bufio.Reader) ([]byte, error) {
	var size uint32
	if err := binary.Read(buf, binary.LittleEndian, &size); err != nil {
		return nil, err
	}

	bytes := make([]byte, size)
	n, err := buf.Read(bytes)
	if err != nil {
		return nil, err
	}
	if uint32(n) != size {
		return nil, fmt.Errorf("unexpected num bytes read. Expected %v, got %v", size, n)
	}
	return bytes, nil
}
