package main

import (
	"log"
	"math"
	"os"
	"os/signal"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/wav"
	"github.com/gordonklaus/portaudio"
)

const (
	framesPerBuf = 512
	channels     = 1 // Set to 2 for stereo
	deviceIndex  = 4
	volume       = 2.0
)

var possibleSampleRates = []float64{44100, 48000, 96000, 16000, 32000, 22050}

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

	var samples []float64
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	log.Printf("Recording from '%s' at %.0fHz", device.Name, sampleRate)
	stream.Start()
	defer stream.Stop()

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
			for _, s := range buffer {
				sample := int16ToFloat64(s) * volume
				if sample > 1.0 {
					sample = 1.0
				} else if sample < -1.0 {
					sample = -1.0
				}
				samples = append(samples, sample)
			}
		}
	}

	format := beep.Format{
		SampleRate:  beep.SampleRate(sampleRate),
		NumChannels: channels,
		Precision:   2,
	}

	if err := wav.Encode(outFile, multiChannelStreamer(samples, channels), format); err != nil {
		log.Fatal(err)
	}

	log.Println("Recording saved")
}

func int16ToFloat64(s int16) float64 {
	return float64(s) / float64(math.MaxInt16)
}

func multiChannelStreamer(samples []float64, ch int) beep.Streamer {
	i := 0
	return beep.StreamerFunc(func(buf [][2]float64) (n int, ok bool) {
		for j := 0; j < len(buf); j++ {
			if i+ch > len(samples) {
				return j, false
			}
			switch ch {
			case 1:
				val := samples[i]
				buf[j][0] = val
				buf[j][1] = val
			case 2:
				buf[j][0] = samples[i]
				buf[j][1] = samples[i+1]
			default:
				sum := 0.0
				for k := 0; k < ch && i+k < len(samples); k++ {
					sum += samples[i+k]
				}
				avg := sum / float64(ch)
				buf[j][0] = avg
				buf[j][1] = avg
			}
			i += ch
		}
		return len(buf), true
	})
}

func findWorkingSampleRate(dev *portaudio.DeviceInfo) (float64, error) {
	for _, rate := range possibleSampleRates {
		params := portaudio.StreamParameters{
			Input: portaudio.StreamDeviceParameters{
				Device:   dev,
				Channels: channels,
				Latency:  dev.DefaultLowInputLatency,
			},
			Output: portaudio.StreamDeviceParameters{
				Channels: 0,
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
