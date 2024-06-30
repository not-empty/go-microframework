package generator

import (
	"fmt"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"os"
	"strings"
)

func (cg *CodeGenerator) ReadFromDb() {
	stubs := GetStubsConfig(cg.Logger, cg.config, cg.domainType)
	content, err := os.ReadFile("tools/migrator/schema/schema.my.hcl")
	if err != nil {
		fmt.Println("Error reading file:", err)
		return
	}
	file, _ := hclwrite.ParseConfig(content, "tools/migrator/schema/schema.my.hcl", hcl.Pos{Line: 1, Column: 1})
	for _, block := range file.Body().Blocks() {
		if block.Type() == "schema" {
			continue
		}
		tableName := block.Labels()[0]
		cg.needImportTime = false
		domain := cg.handleTable(block.Body().Blocks(), tableName)
		cg.config.Replacers["crud"]["{{domainType}}"] = domain
		cg.config.Replacers["crud"]["{{optionalImports}}"] = ""
		if cg.needImportTime {
			cg.config.Replacers["crud"]["{{optionalImports}}"] = `"time"`
		}
		err := cg.validateFiles(domain)
		if err != nil {
			fmt.Println("Error validating files:", err)
			return
		}
		replacers := GetReplacersConfig(cg.config, cg.domainType, []string{tableName})
		cg.GenerateFilesFromStubs(stubs, replacers)
	}
}

func (cg *CodeGenerator) handleTable(blocks []*hclwrite.Block, tableName string) string {
	structString := "type " + PascalCase(tableName) + " struct {\n"
	for _, block := range blocks {
		if block.Type() == "column" {
			token, ok := block.Body().Attributes()["type"]
			if !ok {
				continue
			}
			structString = fmt.Sprintf(
				"%s	%s %s `db:\"%s\"`\n",
				structString,
				PascalCase(block.Labels()[0]),
				cg.dbTypesToGoTypes(string(token.Expr().BuildTokens(nil).Bytes())),
				block.Labels()[0],
			)
		}
	}
	structString += "}"
	return structString
}

func (cg *CodeGenerator) dbTypesToGoTypes(typo string) string {
	dbTypesMap := map[string]string{
		" bigint":     "int64",
		" bit":        "bool",
		" char":       "string",
		" decimal":    "float64",
		" float":      "float32",
		" double":     "float64",
		" int":        "int",
		" longtext":   "string",
		" mediumint":  "int",
		" mediumtext": "string",
		" smallint":   "int16",
		" text":       "string",
		" time":       "time.Time",
		" timestamp":  "time.Time",
		" datetime":   "time.Time",
		" date":       "time.Time",
		" tinyint":    "int8",
		" tinytext":   "string",
		" varbinary":  "string",
		" varchar":    "string",
	}

	GolangType, ok := dbTypesMap[typo]
	if ok {
		return GolangType
	}

	if strings.Contains(typo, "char") {
		return "string"
	}

	if strings.Contains(typo, "time") {
		cg.needImportTime = true
		return "time.Time"
	}
	return typo
}
