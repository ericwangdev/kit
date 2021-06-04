package generator

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/dave/jennifer/jen"
	"github.com/kujtimiihoxha/kit/fs"
	"github.com/kujtimiihoxha/kit/parser"
	"github.com/kujtimiihoxha/kit/utils"
	"github.com/sirupsen/logrus"
)

const (
	// full path to z_<service>.pb.go file, this will be the source to generate dto from
	formatPBGoFileFullPath = `%s/pkg/grpc/pb/z_%s.pb.go`

	// path of dto package, e.g. helloService/pkg/helloService/dto
	formatDTOPackagePath = `%s/pkg/%s/dto`

	// name of the dto file, e.g. z_helloService_dto.go
	formatAutoGenDTOFileName = `z_%s_dto.go`
)

// structState records if a certain struct has been visited
// used during recursively generate dto
type structState struct {
	Struct  parser.Struct
	Visited bool
}

// fieldState records information of a field in a struct
// todo eric.wang currently this does not support nesting such as []map[string]SomeType, consider use reflect
type fieldState struct {
	TypeName     string
	IsStructType bool
	IsMap        bool
	MapKeyType   string
	IsSlice      bool
}

// pbNativeFields contains the name of the pb native fields for each struct in pb.go file
// these fields will be skipped during dto generation
var pbNativeFields = map[string]interface{}{
	"state":         nil,
	"sizeCache":     nil,
	"unknownFields": nil,
}

// GenerateDTOFromProtoGo generates dto structs and grpc bindings for *Request / *Response structs in a pb.go file
// e.g. for a HelloRequest in pb.go file, below will be generated:
// 		type HelloRequest struct {...}, which contains identical fields (excluding pb native fields) of HelloRequest in pb.go
// 		func HelloRequestFromPB(pb *pb.HelloRequest) *HelloRequest {...}, grpc binding to convert from a pb HelloRequest
// 		func HelloRequestToPB(orig *HelloRequest) *pb.HelloRequest {...}, grpc binding to convert to a pb HelloRequest
type GenerateDTOFromProtoGo struct {
	BaseGenerator
	serviceName         string
	protoGoFileFullPath string

	// used to qualify pb package, e.g. pb.SomeStruct
	pbPackagePath string

	// used to qualify dto package, e.g. dto.GeneratedStruct
	dtoPackagePath  string
	dtoFileFullPath string

	// used when generating dto for a specific struct in pb.go
	targetPBStructName string
}

// NewGenerateDTOFromProto ...
func NewGenerateDTOFromProto(serviceName string, targetPBStructName string) Gen {
	i := &GenerateDTOFromProtoGo{
		serviceName:         serviceName,
		protoGoFileFullPath: fmt.Sprintf(formatPBGoFileFullPath, serviceName, serviceName),
		dtoPackagePath:      fmt.Sprintf(formatDTOPackagePath, serviceName, serviceName),
		dtoFileFullPath:     path.Join(fmt.Sprintf(formatDTOPackagePath, serviceName, serviceName), fmt.Sprintf(formatAutoGenDTOFileName, serviceName)),
		targetPBStructName:  targetPBStructName,
		pbPackagePath:       fmt.Sprintf(path.Join("%s", "pkg", "grpc", "pb"), serviceName),
	}

	// init base generator stuff
	i.srcFile = jen.NewFilePath(i.dtoPackagePath)
	i.InitPg()
	i.fs = fs.Get()
	return i
}

func (g *GenerateDTOFromProtoGo) Generate() (err error) {
	// create dto directory if not exist
	if err = g.CreateFolderStructure(g.dtoPackagePath); err != nil {
		logrus.Errorf("failed to create dto directory: %s", err)
		return err
	}

	// ensure pb.go file exists
	if b, err := g.fs.Exists(g.protoGoFileFullPath); err != nil {
		return fmt.Errorf("err checking existing pb.go file path: %s, err: %v", g.protoGoFileFullPath, err)
	} else if !b {
		return fmt.Errorf(" pb.go file does not exist at: %s, need pb.go file to auto gen dto", g.protoGoFileFullPath)
	}

	// parse pb.go file
	pbGoSrc, err := g.fs.ReadFile(g.protoGoFileFullPath)
	if err != nil {
		return fmt.Errorf("err reading pb go file at: %s, err: %v", g.protoGoFileFullPath, err)
	}
	pbGoFile, err := parser.NewFileParser().Parse([]byte(pbGoSrc))
	if err != nil {
		return fmt.Errorf("err parsing pb go file at: %s, err: %v", g.protoGoFileFullPath, err)
	}

	// handle header comment
	g.srcFile.PackageComment("THIS FILE IS AUTO GENERATED, DO NOT EDIT!!")
	g.code.NewLine()

	// generate a manifest of all structs in pb.go file
	// used to avoid generating duplicate dto struct
	pbStructManifest := map[string]*structState{}
	for _, pbStruct := range pbGoFile.Structures {
		pbStructManifest[pbStruct.Name] = &structState{
			Struct:  pbStruct,
			Visited: false,
		}
		logrus.Debug("pb struct manifest: ", pbStruct)
	}

	// loop over all structs in pb.go and generate dto struct for all *Request / *Response as well as their child struct
	for _, pbStruct := range pbGoFile.Structures {
		logrus.Debug("inspecting pb.go struct: ", pbStruct.Name)
		if g.targetPBStructName != "" {
			if pbStruct.Name != g.targetPBStructName {
				logrus.Info("targetPBStructName is provided: ", g.targetPBStructName, ", skipping ", pbStruct.Name)
				continue
			}
		} else {
			if !strings.HasSuffix(pbStruct.Name, "Request") && !strings.HasSuffix(pbStruct.Name, "Response") {
				logrus.Info("skipping struct: ", pbStruct.Name, " only *Request or *Response structs will be considered")
				continue
			}
		}

		g.genDTORecursive(pbStruct, pbStructManifest)
	}

	return g.fs.WriteFile(g.dtoFileFullPath, g.srcFile.GoString(), true)
}

// genDTORecursive is the main func to generate dto structs
// given an input pb struct, do a post-order traverse to generate dto for all its child structs before generating its own
func (g *GenerateDTOFromProtoGo) genDTORecursive(currentPBStruct parser.Struct, pbStructManifest map[string]*structState) {
	if structState, _ := pbStructManifest[currentPBStruct.Name]; structState.Visited {
		logrus.Debug("skip pb struct as it is already visited: ", currentPBStruct)
		return
	}

	logrus.Info("generating dto for: ", currentPBStruct)

	// maintain a manifest for all fields of currentPBStruct
	fieldManifest := map[string]fieldState{}

	dtoFields := []jen.Code{}

	// loop over all fields of pb struct
	for _, field := range currentPBStruct.Vars {
		if _, ok := pbNativeFields[field.Name]; ok {
			logrus.Debug("skipping ", field)
			continue
		}

		logrus.Debug("inspecting field: ", field)
		jsonTagKey, jsonTagVal := utils.JsonTag(field.Name)
		dtoFields = append(dtoFields, jen.Id(field.Name).Id(field.Type).Tag(map[string]string{jsonTagKey: jsonTagVal}))

		fieldType, isSlice, isMap, mapKeyType := parseFieldType(field.Type)
		logrus.Debug("fieldType: ", fieldType, " isSlice: ", isSlice, " isMap: ", isMap, " mapKeyType: ", mapKeyType)

		structState, ok := pbStructManifest[fieldType]
		if !ok {
			// fieldType is not a struct, but can be a map / slice of primitive types, e.g. map[string]string, []string
			fieldManifest[field.Name] = fieldState{
				TypeName:     fieldType,
				IsStructType: false,
				IsSlice:      isSlice,
				IsMap:        isMap,
				MapKeyType:   mapKeyType,
			}
		} else {
			// fieldType is a struct, generate it first then backtrack to current
			fieldManifest[field.Name] = fieldState{
				TypeName:     fieldType,
				IsStructType: true,
				IsSlice:      isSlice,
				IsMap:        isMap,
				MapKeyType:   mapKeyType,
			}

			if !structState.Visited {
				logrus.Debug("recursively gen struct field: ", structState.Struct)
				g.genDTORecursive(structState.Struct, pbStructManifest)
				pbStructManifest[fieldType].Visited = true
			}
		}
	}

	// dto struct name is the same as pb go struct name
	g.code.appendStruct(currentPBStruct.Name, dtoFields...)
	pbStructManifest[currentPBStruct.Name].Visited = true

	g.genBindingFromPB(currentPBStruct.Name, fieldManifest)
	g.genBindingToPB(currentPBStruct.Name, fieldManifest)
}

func (g *GenerateDTOFromProtoGo) genBindingFromPB(currentPBStructName string, fieldManifest map[string]fieldState) {
	funcBodyForFromPB := []jen.Code{
		jen.If(jen.Id("pb").Id("==").Nil()).
			Block(jen.Return(jen.Nil())).Line(),
	}
	assignmentsForFromPB := jen.Dict{}

	for fieldName, fieldState := range fieldManifest {
		logrus.Debug("genBindingFromPB: ", "field name: ", fieldName, " fieldState: ", fieldState)

		// if field is not a struct, only need assignment line:
		// `AStringField := pb.AStringField`
		if !fieldState.IsStructType {
			assignmentsForFromPB[jen.Id(fieldName)] = jen.Id("pb").Dot(fieldName)
			continue
		}

		if fieldState.IsMap {
			// m := make(map[string]*Address, len(pb.Addresses))
			// for k, v := range pb.Addresses {
			//		m[k] = AddressFromPB(v)
			//}
			funcBodyForFromPB = append(funcBodyForFromPB,
				jen.Id("m").Op(":=").Make(jen.Map(jen.Id(fieldState.MapKeyType)).Id("*").Qual(g.dtoPackagePath, fieldState.TypeName), jen.Len(jen.Id("pb").Dot(fieldName))),
				jen.For(
					jen.Id("k").Op(`,`).Id("v").Op(":=").Range().Qual(g.pbPackagePath, fieldName).
						Block(jen.Id("m").Index(jen.Id("k")).Op("=").Id(fieldState.TypeName+"FromPB").Call(jen.Id("v")))),
			)

			// Addresses = m
			assignmentsForFromPB[jen.Id(fieldName)] = jen.Id("m")
		} else if fieldState.IsSlice {
			// aSlice := make([]*Address, 0, len(pb.Addresses))
			// for _, v := range pb.Addresses {
			//		aSlice = append(aSlice, AddressFromPB(v))
			//}
			funcBodyForFromPB = append(funcBodyForFromPB,
				jen.Id("aSlice").Op(":=").Make(jen.Index().Id("*").Qual(g.dtoPackagePath, fieldState.TypeName), jen.Lit(0), jen.Len(jen.Id("pb").Dot(fieldName))),
				jen.For(
					jen.Id("_").Op(`,`).Id("v").Op(":=").Range().Qual(g.pbPackagePath, fieldName).
						Block(jen.Id("aSlice").Op("=").Append(jen.Id("aSlice"), jen.Id(fieldState.TypeName+"FromPB").Call(jen.Id("v"))))),
			)

			// Addresses = aSlice
			assignmentsForFromPB[jen.Id(fieldName)] = jen.Id("aSlice")
		} else {
			// field is a single struct, we add only assignment:
			// Address = AddressFromPB(pb.Address)
			assignmentsForFromPB[jen.Id(fieldName)] = jen.Id(fieldState.TypeName + "FromPB").Call(jen.Id("pb").Dot(fieldName))
		}
	}

	// add assignments to the end of func body
	funcBodyForFromPB = append(funcBodyForFromPB, jen.Return(jen.Id("&").Qual(g.dtoPackagePath, currentPBStructName).Values(assignmentsForFromPB)))

	g.code.appendFunction(
		fmt.Sprintf("%sFromPB", currentPBStructName),
		nil,
		[]jen.Code{
			jen.Id("pb").Id("*").Qual(g.pbPackagePath, currentPBStructName),
		},
		[]jen.Code{
			jen.Id("").Id("*").Qual(g.dtoPackagePath, currentPBStructName),
		},
		"",
		funcBodyForFromPB...,
	)
	g.code.NewLine()
	g.code.NewLine()
}

func (g *GenerateDTOFromProtoGo) genBindingToPB(currentPBStructName string, fieldManifest map[string]fieldState) {
	funcBodyForToPB := []jen.Code{
		jen.If(jen.Id("orig").Id("==").Nil()).
			Block(jen.Return(jen.Nil())).Line(),
	}
	assignmentsForToPB := jen.Dict{}

	for fieldName, fieldState := range fieldManifest {
		logrus.Debug("genBindingToPB: ", "field name: ", fieldName, " fieldState: ", fieldState)

		// if field is not a struct, only need assignment line:
		// `AStringField := pb.AStringField`
		if !fieldState.IsStructType {
			assignmentsForToPB[jen.Id(fieldName)] = jen.Id("orig").Dot(fieldName)
			continue
		}

		if fieldState.IsMap {
			// m := make(map[string]*pb.Address, len(orig.Addresses))
			// for k, v := range orig.Addresses {
			//		m[k] = AddressToPB(v)
			//}
			funcBodyForToPB = append(funcBodyForToPB,
				jen.Id("m").Op(":=").Make(jen.Map(jen.Id(fieldState.MapKeyType)).Id("*").Qual(g.pbPackagePath, fieldState.TypeName), jen.Len(jen.Id("orig").Dot(fieldName))),
				jen.For(
					jen.Id("k").Op(`,`).Id("v").Op(":=").Range().Id("orig").Dot(fieldName).
						Block(jen.Id("m").Index(jen.Id("k")).Op("=").Id(fieldState.TypeName+"ToPB").Call(jen.Id("v")))),
			)
			// Addresses = m
			assignmentsForToPB[jen.Id(fieldName)] = jen.Id("m")
		} else if fieldState.IsSlice {
			// aSlice := make([]*pb.Address, 0, len(orig.Addresses))
			// for _, v := range orig.Addresses {
			//		aSlice = append(aSlice, AddressToPB(v))
			//}
			funcBodyForToPB = append(funcBodyForToPB,
				jen.Id("aSlice").Op(":=").Make(jen.Index().Id("*").Qual(g.pbPackagePath, fieldState.TypeName), jen.Lit(0), jen.Len(jen.Id("orig").Dot(fieldName))),
				jen.For(
					jen.Id("_").Op(`,`).Id("v").Op(":=").Range().Id("orig").Dot(fieldName).
						Block(jen.Id("aSlice").Op("=").Append(jen.Id("aSlice"), jen.Id(fieldState.TypeName+"ToPB").Call(jen.Id("v"))))),
			)

			// Addresses = aSlice
			assignmentsForToPB[jen.Id(fieldName)] = jen.Id("aSlice")
		} else {
			// field is a single struct, we add only assignment:
			// Address = AddressToPB(pb.Address)
			assignmentsForToPB[jen.Id(fieldName)] = jen.Id(fieldState.TypeName + "ToPB").Call(jen.Id("orig").Dot(fieldName))
		}
	}

	// add assignments to the end of func body
	funcBodyForToPB = append(funcBodyForToPB, jen.Return(jen.Id("&").Qual(g.pbPackagePath, currentPBStructName).Values(assignmentsForToPB)))

	// gen *ToPB func, e.g. InitApplicationRequestToPB
	g.code.appendFunction(
		fmt.Sprintf("%sToPB", currentPBStructName),
		nil,
		[]jen.Code{
			jen.Id("orig").Id("*").Qual(g.dtoPackagePath, currentPBStructName),
		},
		[]jen.Code{
			jen.Id("").Id("*").Qual(g.pbPackagePath, currentPBStructName),
		},
		"",
		funcBodyForToPB...,
	)
	g.code.NewLine()
}

func fieldIsAMap(typeName string) bool {
	return strings.Contains(typeName, `map[`)
}

func fieldIsASlice(typeName string) bool {
	return strings.Contains(typeName, `[]`)
}

// todo eric.wang, this function assumes typeName can only be struct, plain slice or plain map, nested types such as slice of maps or map of slices are not supported yet and will cause weird output
func parseFieldType(typeName string) (nameNoStar string, isSlice bool, isMap bool, mapKeyType string) {
	if fieldIsASlice(typeName) {
		isSlice = true
		parts := strings.Split(typeName, `]`)
		nameNoStar = strings.TrimPrefix(parts[1], `*`)
	} else if fieldIsAMap(typeName) {
		isMap = true
		parts := strings.Split(typeName, `]`)
		nameNoStar = strings.TrimPrefix(parts[1], `*`)
		mapKeyType = getBetweenBrackets(typeName)
	} else {
		nameNoStar = strings.TrimPrefix(typeName, `*`)
	}
	return nameNoStar, isSlice, isMap, mapKeyType
}

func getBetweenBrackets(s string) string {
	re := regexp.MustCompile(`\[([^\[\]]*)\]`)
	allMatches := re.FindAllString(s, -1)
	firstMatch := allMatches[0]
	trimLeft := strings.Trim(firstMatch, `[`)
	trimRight := strings.Trim(trimLeft, `]`)
	return trimRight
}
