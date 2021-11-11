package main

type ImageConsumer struct {
    consumer  func( string, []byte ) (error)
    noframesf func()
    udid      string
}

func NewImageConsumer( consumer func( string, []byte ) (error), noframes func() ) (*ImageConsumer) {
    self := &ImageConsumer{
        consumer: consumer,
        noframesf: noframes,
    }
    return self
}

func (self *ImageConsumer) consume( text string, bytes []byte ) (error) {
    return self.consumer( text, bytes )
}

func (self *ImageConsumer) noframes() {
    self.noframesf()
}