package main

func main() {
	if err := NewServer("certs", true).Serve(); err != nil {
		panic(err)
	}
}
