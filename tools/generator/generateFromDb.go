package generator

import (
	"fmt"
	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"os"
	"strings"
)

func (cg *CodeGenerator) ReadFromSchema(schema string, table string) {
	file, hclErr := cg.getHclFile(schema)
	if hclErr != nil {
		fmt.Println("Error validating files:", hclErr)
		return
	}
	for _, block := range file.Body().Blocks() {
		if table != "" && block.Labels()[0] != table {
			continue
		}
		err := cg.handleHclBlock(block)
		if err != nil {
			fmt.Println("Error validating files:", err)
			return
		}
	}
}

func (cg *CodeGenerator) getHclFile(schema string) (*hclwrite.File, error) {
	filepath := fmt.Sprintf("schemas/%s.my.hcl", schema)
	content, err := os.ReadFile(filepath)
	if err != nil {
		fmt.Println("Error reading file:", err)
		return nil, err
	}
	file, _ := hclwrite.ParseConfig(content, filepath, hcl.Pos{Line: 1, Column: 1})
	return file, err
}

func (cg *CodeGenerator) handleHclBlock(block *hclwrite.Block) error {
	if block.Type() == "schema" {
		return nil
	}
	if len(block.Labels()) == 0 {
		return nil
	}

	tableName := CamelCase(block.Labels()[0])
	rawTableName := block.Labels()[0]
	replacers := cg.generateDomainFromHclBlock(block, tableName, rawTableName)
	validateErr := cg.validateFiles(tableName)
	if validateErr != nil {
		return validateErr
	}
	stubs := GetStubsConfig(cg.Logger, cg.config, cg.domainType)
	cg.GenerateFilesFromStubs(stubs, replacers)
	return nil
}

func (cg *CodeGenerator) generateDomainFromHclBlock(block *hclwrite.Block, tableName string, rawTableName string) map[string]string {
	cg.needImportTime = new(bool)
	cg.primaryKey = new(string)
	cg.pkType = new(string)
	cg.isIntId = new(bool)
	*cg.needImportTime = false
	*cg.isIntId = false
	domain := cg.generateDomainStruct(block.Body().Blocks(), tableName, cg.primaryKey, cg.pkType)
	dataType := cg.generateStruct(block.Body().Blocks(), nil, nil, cg.generateDeclarationLine)
	createAttrData := cg.generateStruct(block.Body().Blocks(), nil, nil, cg.generateAttributionLine)
	editAttrData := cg.generateStruct(block.Body().Blocks(), nil, nil, cg.generateAttributionLine)
	replacers := GetReplacersConfig(cg.config, cg.domainType, []string{tableName, rawTableName})
	replacers["{{domainType}}"] = domain
	replacers["{{dataType}}"] = dataType
	replacers["{{pkDbName}}"] = *cg.primaryKey
	replacers["{{pkName}}"] = *cg.primaryKey
	replacers["{{pkType}}"] = *cg.pkType
	replacers["{{createServiceData}}"] = createAttrData
	replacers["{{editServiceData}}"] = editAttrData
	if *cg.needImportTime {
		replacers["{{optionalImports}}"] = "\"time\""
	}
	if !*cg.isIntId {
		replacers["{{idVar}}"] = "domain." + PascalCase(*cg.primaryKey) + " = s.idCreator.Create()"
	}
	return replacers
}

func (cg *CodeGenerator) generateDomainStruct(blocks []*hclwrite.Block, tableName string, pk, pkType *string) string {
	*pk = cg.findPkOnBlocks(blocks)
	structString := "type " + PascalCase(tableName) + " struct {\n"
	structString += cg.generateStruct(blocks, pk, pkType, cg.generateDeclarationLine)
	structString += "\tclient string\n\tfilters *filters.Filters\n"
	structString += "}"
	return structString
}

func (cg *CodeGenerator) generateStruct(blocks []*hclwrite.Block, pk, pkType *string, strFormationFunc func(string, string, string, string) string) string {
	declarationString := ""
	for _, block := range blocks {
		if block.Type() == "column" {
			token, ok := block.Body().Attributes()["type"]
			if !ok {
				continue
			}
			tokenStr := string(token.Expr().BuildTokens(nil).Bytes())
			goType := cg.dbTypesToGoTypes(tokenStr)
			nullable, nullOk := block.Body().Attributes()["null"]
			isNullable := cg.verifyIsNullable(nullable, nullOk)
			if isNullable {
				goType = "*" + goType
			}

			if pk != nil && block.Labels()[0] == *pk {
				*pkType = fmt.Sprintf("%s string `param:\"id\"`\n", PascalCase(*pk))
			}

			declarationString = strFormationFunc(
				declarationString,
				PascalCase(block.Labels()[0]),
				goType,
				block.Labels()[0],
			)
		}
	}
	return declarationString
}

func (cg *CodeGenerator) generateDeclarationLine(str, name, goType, dbTag string) string {
	if name == PascalCase(*cg.primaryKey) && strings.Contains(goType, "int") {
		return fmt.Sprintf(
			"%s	%s %s `db:\"%s\"`\n",
			str,
			name,
			"*string",
			dbTag,
		)
	}
	return fmt.Sprintf(
		"%s	%s %s `db:\"%s\"`\n",
		str,
		name,
		goType,
		dbTag,
	)
}

func (cg *CodeGenerator) generateAttributionLine(str, name, _, _ string) string {
	if name == PascalCase(*cg.primaryKey) {
		return str
	}
	return fmt.Sprintf(
		"%s	domain.%s = data.%s\n",
		str,
		name,
		name,
	)
}

func (cg *CodeGenerator) findPkOnBlocks(blocks []*hclwrite.Block) string {
	str := ""
	for _, block := range blocks {
		if block.Type() == "primary_key" {
			token, ok := block.Body().Attributes()["columns"]
			if !ok {
				return ""
			}
			pkAttr := string(token.Expr().BuildTokens(nil).Bytes())
			str = cg.getColumnFromAttrString(pkAttr)
		}
	}
	return PascalCase(str)
}

func (cg *CodeGenerator) getColumnFromAttrString(attrStr string) string {
	str := strings.Replace(attrStr, "[", "", -1)
	str = strings.Replace(str, "]", "", -1)
	str = strings.Split(str, ".")[1]
	return str
}

func (cg *CodeGenerator) dbTypesToGoTypes(typo string) string {
	dbTypesMap := map[string]string{
		" bigint":     "int64",
		" bit":        " ",
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
		" time":       "string",
		" timestamp":  "string",
		" datetime":   "time.Time",
		" date":       "string",
		" tinyint":    "int8",
		" tinytext":   "string",
		" varbinary":  "string",
		" varchar":    "string",
		" json":       "string",
	}

	GolangType, ok := dbTypesMap[typo]
	if ok {
		if GolangType == "time.Time" {
			*cg.needImportTime = true
		}
		return GolangType
	}

	if strings.Contains(typo, "char") {
		return "string"
	}

	if strings.Contains(typo, "double") {
		return "float64"
	}

	if strings.Contains(typo, "year") {
		return "string"
	}

	if strings.Contains(typo, "decimal") {
		return "float64"
	}

	return typo
}

func (cg *CodeGenerator) verifyIsNullable(token *hclwrite.Attribute, ok bool) bool {
	if !ok {
		return false
	}
	value := token.Expr().BuildTokens(nil).Bytes()
	if string(value) == "true" {
		return true
	}
	return false
}
