package generator

import (
	"fmt"
	"testing"

	"github.com/dave/jennifer/jen"
	"github.com/kujtimiihoxha/kit/fs"
	"github.com/kujtimiihoxha/kit/parser"
	"github.com/stretchr/testify/assert"
)

func TestGenerateDTO(t *testing.T) {
	setDefaults()
	type fields struct {
		BaseGenerator       BaseGenerator
		name                string
		protoGoFileFullPath string
		dtoPackagePath      string
		dtoFileFullPath     string
		targetPBStructName  string
		pbPackagePath       string
		file                *parser.File
		serviceInterface    parser.Interface
	}

	tests := []struct {
		name       string
		fields     fields
		wantErr    bool
		wantResult string
	}{
		{
			name: "generator throws error when no pb.go file",
			fields: fields{
				BaseGenerator: func() BaseGenerator {
					b := BaseGenerator{}
					b.srcFile = jen.NewFilePath("")
					b.InitPg()
					b.fs = fs.NewDefaultFs("")
					return b
				}(),
				name:                "test",
				protoGoFileFullPath: "test/pkg/grpc/pb/z_test.pb.go",
				dtoPackagePath:      "test/pkg/test/dto",
				dtoFileFullPath:     "test/pkg/test/dto/z_test_dto.go",
			},
			wantErr: true,
		},
		{
			name: "generator throws error when pb.go file is invalid",
			fields: fields{
				BaseGenerator: func() BaseGenerator {
					b := BaseGenerator{}
					b.srcFile = jen.NewFilePath("")
					b.InitPg()
					f := fs.NewDefaultFs("")
					f.MkdirAll("test/pkg/grpc/pb/z_test.pb.go")
					f.WriteFile("test/pkg/grpc/pb/z_test.pb.go", `random content`, true)
					b.fs = f
					return b
				}(),
				name:                "test",
				protoGoFileFullPath: "test/pkg/grpc/pb/z_test.pb.go",
				dtoPackagePath:      "test/pkg/test/dto",
				dtoFileFullPath:     "test/pkg/test/dto/z_test_dto.go",
			},
			wantErr: true,
		},
		{
			name: "generator does nothing if pb.go file contains no *Request / *Response struct",
			fields: fields{
				BaseGenerator: func() BaseGenerator {
					b := BaseGenerator{}
					b.srcFile = jen.NewFilePath("dto")
					b.InitPg()
					f := fs.NewDefaultFs("")
					f.MkdirAll("test/pkg/grpc/pb/z_test.pb.go")
					f.WriteFile("test/pkg/grpc/pb/z_test.pb.go", `package service
					import "context"
					type Test struct{
					}`, true)
					b.fs = f
					return b
				}(),
				name:                "test",
				protoGoFileFullPath: "test/pkg/grpc/pb/z_test.pb.go",
				dtoPackagePath:      "test/pkg/test/dto",
				dtoFileFullPath:     "test/pkg/test/dto/z_test_dto.go",
			},
			wantErr: false,
			wantResult: "// THIS FILE IS AUTO GENERATED, DO NOT EDIT!!\npackage dto\n",
		},
		{
			name: "generator creates dto struct + bindings if pb.go file contains *Request / *Response struct",
			fields: fields{
				BaseGenerator: func() BaseGenerator {
					b := BaseGenerator{}
					b.srcFile = jen.NewFilePath("test/pkg/test/dto")
					b.InitPg()
					f := fs.NewDefaultFs("")
					f.MkdirAll("test/pkg/grpc/pb/z_test.pb.go")
					f.WriteFile("test/pkg/grpc/pb/z_test.pb.go", `package pb
					type TestRequest struct{
					}`, true)
					b.fs = f
					return b
				}(),
				name:                "test",
				protoGoFileFullPath: "test/pkg/grpc/pb/z_test.pb.go",
				dtoPackagePath:      "test/pkg/test/dto",
				dtoFileFullPath:     "test/pkg/test/dto/z_test_dto.go",
				pbPackagePath:       "test/pkg/grpc/pb",
			},
			wantErr: false,
			wantResult: `// THIS FILE IS AUTO GENERATED, DO NOT EDIT!!
package dto

import pb "test/pkg/grpc/pb"

type TestRequest struct{}

func TestRequestFromPB(pb *pb.TestRequest) *TestRequest {
	if pb == nil {
		return nil
	}

	return &TestRequest{}
}

func TestRequestToPB(orig *TestRequest) *pb.TestRequest {
	if orig == nil {
		return nil
	}

	return &pb.TestRequest{}
}
`,
		},
		{
			name: "generator creates dto struct + bindings if target pb struct exists in pb.go, and only create for that struct",
			fields: fields{
				BaseGenerator: func() BaseGenerator {
					b := BaseGenerator{}
					b.srcFile = jen.NewFilePath("test/pkg/test/dto")
					b.InitPg()
					f := fs.NewDefaultFs("")
					f.MkdirAll("test/pkg/grpc/pb/z_test.pb.go")
					f.WriteFile("test/pkg/grpc/pb/z_test.pb.go", `package pb
					type Something struct{
						Name string
						StructMap map[string]*StructVal
						StructSlice []*StructVal
					}

					type StructVal struct {
						AString string
					}
	
					type Nothing struct {}
					type ARequest struct {}
`, true)
					b.fs = f
					return b
				}(),
				name:                "test",
				protoGoFileFullPath: "test/pkg/grpc/pb/z_test.pb.go",
				dtoPackagePath:      "test/pkg/test/dto",
				dtoFileFullPath:     "test/pkg/test/dto/z_test_dto.go",
				pbPackagePath:       "test/pkg/grpc/pb",
				targetPBStructName:  "Something",
			},
			wantErr: false,
			wantResult: `// THIS FILE IS AUTO GENERATED, DO NOT EDIT!!
package dto

import pb "test/pkg/grpc/pb"

type StructVal struct {
	AString string` + " `json:\"aString\"`\n}"+`

func StructValFromPB(pb *pb.StructVal) *StructVal {
	if pb == nil {
		return nil
	}

	return &StructVal{AString: pb.AString}
}

func StructValToPB(orig *StructVal) *pb.StructVal {
	if orig == nil {
		return nil
	}

	return &pb.StructVal{AString: orig.AString}
}

type Something struct {
	Name        string                ` + "`json:\"name\"`"+`
	StructMap   map[string]*StructVal ` + "`json:\"structMap\"`" +`
	StructSlice []*StructVal          ` + "`json:\"structSlice\"`" + `
}

func SomethingFromPB(pb *pb.Something) *Something {
	if pb == nil {
		return nil
	}

	m := make(map[string]*StructVal, len(pb.StructMap))
	for k, v := range pb.StructMap {
		m[k] = StructValFromPB(v)
	}
	aSlice := make([]*StructVal, 0, len(pb.StructSlice))
	for _, v := range pb.StructSlice {
		aSlice = append(aSlice, StructValFromPB(v))
	}
	return &Something{
		Name:        pb.Name,
		StructMap:   m,
		StructSlice: aSlice,
	}
}

func SomethingToPB(orig *Something) *pb.Something {
	if orig == nil {
		return nil
	}

	m := make(map[string]*pb.StructVal, len(orig.StructMap))
	for k, v := range orig.StructMap {
		m[k] = StructValToPB(v)
	}
	aSlice := make([]*pb.StructVal, 0, len(orig.StructSlice))
	for _, v := range orig.StructSlice {
		aSlice = append(aSlice, StructValToPB(v))
	}
	return &pb.Something{
		Name:        orig.Name,
		StructMap:   m,
		StructSlice: aSlice,
	}
}
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &GenerateDTOFromProtoGo{
				BaseGenerator: tt.fields.BaseGenerator,
				serviceName:   tt.fields.name,
			}
			g.protoGoFileFullPath = tt.fields.protoGoFileFullPath
			g.dtoPackagePath = tt.fields.dtoPackagePath
			g.dtoFileFullPath = tt.fields.dtoFileFullPath
			g.pbPackagePath = tt.fields.pbPackagePath
			g.targetPBStructName = tt.fields.targetPBStructName
			if err := g.Generate(); (err != nil) != tt.wantErr {
				t.Errorf("GenerateTransport.Generate() error = %v, wantErr %v", err, tt.wantErr)
			}
			content, _ := g.fs.ReadFile(tt.fields.dtoFileFullPath)
			fmt.Println(content)
			assert.Equal(t, tt.wantResult, content)
		})
	}
}
