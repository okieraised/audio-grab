package main

import (
	"encoding/binary"
	"log"
	"math"
	"os"
	"os/signal"

	"github.com/gordonklaus/portaudio"
)

const (
	framesPerBuf  = 512
	channels      = 1
	deviceIndex   = 4
	volume        = 1.0
	bitsPerSample = 16
)

var possibleSampleRates = []float64{44100, 48000, 16000, 32000}

func main() {
	if err := portaudio.Initialize(); err != nil {
		log.Fatal(err)
	}
	defer portaudio.Terminate()

	devices, err := portaudio.Devices()
	if err != nil {
		log.Fatal(err)
	}

	for i, dev := range devices {
		if dev.MaxInputChannels >= channels {
			log.Printf("Device #%d: %s", i, dev.Name)
		}
	}

	device := devices[deviceIndex]

	sampleRate, err := findWorkingSampleRate(device)
	if err != nil {
		log.Fatalf("No working sample rate found: %v", err)
	}

	buffer := make([]int16, framesPerBuf*channels)

	params := portaudio.StreamParameters{
		Input: portaudio.StreamDeviceParameters{
			Device:   device,
			Channels: channels,
			Latency:  device.DefaultLowInputLatency,
		},
		SampleRate:      sampleRate,
		FramesPerBuffer: framesPerBuf,
	}

	stream, err := portaudio.OpenStream(params, buffer)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	outFile, err := os.Create("micdropper.wav")
	if err != nil {
		log.Fatal(err)
	}
	defer outFile.Close()

	// Write placeholder WAV header
	if err := writeWavHeader(outFile, uint32(sampleRate), uint16(channels), bitsPerSample, 0); err != nil {
		log.Fatal(err)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	log.Printf("Recording at %.0f Hz... Press Ctrl+C to stop", sampleRate)

	var totalBytesWritten int

	if err := stream.Start(); err != nil {
		log.Fatal(err)
	}
	defer stream.Stop()

	// Apply volume and clamp
	writeSample := func(s int16) {
		sample := int16(float64(s) * volume)
		if err := binary.Write(outFile, binary.LittleEndian, sample); err != nil {
			log.Fatal(err)
		}
	}

recordingLoop:
	for {
		select {
		case <-stop:
			log.Println("Stopping...")
			break recordingLoop
		default:
			if err := stream.Read(); err != nil {
				log.Fatal(err)
			}
			for i := 0; i < len(buffer); i += channels {
				var left, right int16

				switch channels {
				case 1:
					val := buffer[i]
					writeSample(val)
				case 2:
					left = buffer[i]
					right = buffer[i+1]

					writeSample(left)
					writeSample(right)
					totalBytesWritten += 4 // 2 bytes per channel
				default:
					// Downmix >2 channels by averaging
					sum := 0
					for ch := 0; ch < channels && i+ch < len(buffer); ch++ {
						sum += int(buffer[i+ch])
					}
					avg := int16(sum / channels)
					left, right = avg, avg

					writeSample(left)
					writeSample(right)
					totalBytesWritten += 4 // 2 bytes per channel
				}

			}

		}
	}

	// Go back and update the WAV header with correct data size
	if err := updateWavHeader(outFile, totalBytesWritten); err != nil {
		log.Fatal(err)
	}

	log.Println("Recording saved to micdropper.wav")
}

func int16ToFloat64(s int16) float64 {
	return float64(s) / float64(math.MaxInt16)
}

func findWorkingSampleRate(dev *portaudio.DeviceInfo) (float64, error) {
	for _, rate := range possibleSampleRates {
		params := portaudio.StreamParameters{
			Input: portaudio.StreamDeviceParameters{
				Device:   dev,
				Channels: channels,
				Latency:  dev.DefaultLowInputLatency,
			},
			SampleRate:      rate,
			FramesPerBuffer: framesPerBuf,
		}
		if err := portaudio.IsFormatSupported(params, []int16{}); err == nil {
			return rate, nil
		}
	}
	return 0, os.ErrInvalid
}

func writeWavHeader(w *os.File, sampleRate uint32, numChannels uint16, bitsPerSample int, dataSize uint32) error {
	blockAlign := numChannels * uint16(bitsPerSample) / 8
	byteRate := sampleRate * uint32(blockAlign)

	// RIFF header
	_, err := w.Write([]byte("RIFF"))
	if err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(36+dataSize)); err != nil { // file size placeholder
		return err
	}
	_, err = w.Write([]byte("WAVE"))
	if err != nil {
		return err
	}

	// fmt chunk
	_, err = w.Write([]byte("fmt "))
	if err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(16)); err != nil { // size of fmt chunk
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(1)); err != nil { // PCM format
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, numChannels); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, sampleRate); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, byteRate); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, blockAlign); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint16(bitsPerSample)); err != nil {
		return err
	}

	// data chunk header
	_, err = w.Write([]byte("data"))
	if err != nil {
		return err
	}
	return binary.Write(w, binary.LittleEndian, dataSize) // placeholder
}

func updateWavHeader(w *os.File, dataSize int) error {
	if _, err := w.Seek(4, 0); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(36+dataSize)); err != nil {
		return err
	}
	if _, err := w.Seek(40, 0); err != nil {
		return err
	}
	return binary.Write(w, binary.LittleEndian, uint32(dataSize))
}
