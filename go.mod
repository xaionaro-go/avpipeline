module github.com/xaionaro-go/avpipeline

go 1.24.1

replace github.com/asticode/go-astiav v0.36.0 => github.com/xaionaro-go/astiav v0.0.0-20250521203320-7402f3e25a7c

replace github.com/xaionaro-go/observability => ../observability

replace github.com/xaionaro-go/avcommon => ../avcommon

replace github.com/xaionaro-go/sockopt => ../sockopt

require (
	github.com/asticode/go-astiav v0.36.0
	github.com/asticode/go-astikit v0.54.0
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc
	github.com/facebookincubator/go-belt v0.0.0-20250308011339-62fb7027b11f
	github.com/go-ng/xatomic v0.0.0-20230519181013-85c0ec87e55f
	github.com/go-ng/xsort v0.0.0-20250330112557-d2ee7f01661c
	github.com/spf13/pflag v1.0.6
	github.com/stretchr/testify v1.10.0
	github.com/xaionaro-go/avcommon v0.0.0-20250510235605-840f8210b727
	github.com/xaionaro-go/avmediacodec v0.0.0-20250505012527-c819676502d8
	github.com/xaionaro-go/libsrt v0.0.0-20250505013920-61d894a3b7e9
	github.com/xaionaro-go/observability v0.0.0-20250525153415-e6c2d935ab34
	github.com/xaionaro-go/proxy v0.0.0-20250525144747-579f5a891c15
	github.com/xaionaro-go/secret v0.0.0-20250111141743-ced12e1082c2
	github.com/xaionaro-go/sockopt v0.0.0-00010101000000-000000000000
	github.com/xaionaro-go/typing v0.0.0-20221123235249-2229101d38ba
	github.com/xaionaro-go/unsafetools v0.0.0-20241024014258-a46e1ce3763e
	github.com/xaionaro-go/xcontext v0.0.0-20250111150717-e70e1f5b299c
	github.com/xaionaro-go/xsync v0.0.0-20250511184922-deec5fb01a0f
	google.golang.org/grpc v1.72.1
	google.golang.org/protobuf v1.36.5
)

require (
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/xaionaro-go/ndk v0.0.0-20250420195304-361bb98583bf // indirect
	golang.org/x/net v0.39.0 // indirect
	golang.org/x/text v0.25.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20250218202821-56aae31c358a // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

require (
	github.com/DataDog/gostackparse v0.7.0 // indirect
	github.com/go-ng/container v0.0.0-20220615121757-4740bf4bbc52
	github.com/go-ng/slices v0.0.0-20230703171042-6195d35636a2 // indirect
	github.com/go-ng/sort v0.0.0-20220617173827-2cc7cd04f7c7 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/huandu/go-tls v1.0.1 // indirect
	github.com/klauspost/cpuid/v2 v2.2.10 // indirect
	github.com/phuslu/goid v1.0.2 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	github.com/xaionaro-go/gorex v0.0.0-20241010205749-bcd59d639c4d // indirect
	github.com/xaionaro-go/logrustash v0.0.0-20240804141650-d48034780a5f // indirect
	github.com/xaionaro-go/object v0.0.0-20241026212449-753ce10ec94c // indirect
	github.com/xaionaro-go/spinlock v0.0.0-20200518175509-30e6d1ce68a1 // indirect
	golang.org/x/crypto v0.38.0 // indirect
	golang.org/x/exp v0.0.0-20250408133849-7e4ce0ab07d0
	golang.org/x/sys v0.33.0 // indirect
	lukechampine.com/blake3 v1.4.0 // indirect
)
