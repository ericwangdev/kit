package cmd

import (
	"github.com/kujtimiihoxha/kit/generator"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var genDTOCommand = &cobra.Command{
	Use:     "dto",
	Short:   "Generate dto from pb.go",
	Aliases: []string{"dto"},
	Run: func(cmd *cobra.Command, args []string) {
		var (
			service            = viper.GetString("targetService")
			targetPBStructName = viper.GetString("targetPBStruct")
		)

		if len(service) == 0 {
			logrus.Error("you must provide a name for the service")
			return
		}

		logrus.Info("will look for pb.go for service: ", service)

		logrus.Warn(
			`current limitations: 
	1. a pb.go file need to be created prior to running this command;
	2. command does NOT support in-place update and will fail if <serviceName>/pkg/<serviceName>/dto/z_<serviceName>_dto.go already exists;
	3. for collection types, only plain map and slice are supported, nested collections such as map[string][]string or []map[string]string are not supported.`)

		if targetPBStructName != "" {
			logrus.Info("targeting specific struct in pb.go: ", targetPBStructName)
		} else {
			logrus.Info("no target struct is specified, will generate for all *Request/*Response structs in pb.go")
		}

		g := generator.NewGenerateDTOFromProto(service, targetPBStructName)
		if err := g.Generate(); err != nil {
			logrus.Error(err)
		}
	},
}

func init() {
	generateCmd.AddCommand(genDTOCommand)
	genDTOCommand.Flags().StringP("targetService", "s", "", "Name of the service")
	genDTOCommand.Flags().StringP("targetPBStruct", "x", "", "Name of the target struct in pb.go that you want to generate dto for")

	viper.BindPFlag("targetService", genDTOCommand.Flags().Lookup("targetService"))
	viper.BindPFlag("targetPBStruct", genDTOCommand.Flags().Lookup("targetPBStruct"))
}
