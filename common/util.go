package common

import (
	"bytes"
	"errors"
	"github.com/younamebert/xlibp2p/common/ahash"
	"github.com/younamebert/xlibp2p/common/rawencode"
	"math"
)

func ObjSHA256(obj rawencode.RawEncoder) ([]byte, []byte, error) {
	txData, err := rawencode.Encode(obj)
	if err != nil {
		return nil, nil, err
	}
	txHash := ahash.SHA256(txData)
	return txData, txHash, nil
}

func BytesMixed(src []byte, lenBits int, buffer *bytes.Buffer) error {
	srcLen := len(src)
	if uint32(srcLen) > uint32(math.MaxUint32) {
		return errors.New("data to long")
	}
	var lenBuf [4]byte
	lenBuf[0] = uint8(srcLen & 0xff)
	lenBuf[1] = uint8((srcLen & 0xff00) >> 8)
	lenBuf[2] = uint8((srcLen & 0xff0000) >> 16)
	lenBuf[3] = uint8((srcLen & 0xff000000) >> 32)
	buffer.Write(lenBuf[0:lenBits])
	buffer.Write(src)
	return nil
}

func ReadMixedBytes(buf *bytes.Buffer) ([]byte, error) {
	dataLenB, err := buf.ReadByte()
	if err != nil {
		return nil, err
	}
	dataLen := int(dataLenB)
	var dst = make([]byte, dataLen)
	_, err = buf.Read(dst)
	if err != nil {
		return nil, err
	}
	return dst, nil
}
