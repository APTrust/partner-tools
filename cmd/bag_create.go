package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/APTrust/dart-runner/bagit"
	"github.com/APTrust/dart-runner/util"
	"github.com/spf13/cobra"
)

var manifestAlgs []string
var userSuppliedTags []string

// createCmd represents the create command
var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a BagIt bag",
	Long: `Package a directory into a BagIt bag using a specific
BagIt profile, manifest algorithms, and tag values. The example
below demonstrates how to speficy flags and tag values.

For tag values, use the format "filename.txt/Tag-Name=tag value". If you
omit the file name, it defaults to bag-info.txt. For example, the following
two tags will both be written into the Source-Organization tag in 
bag-info.txt:

  bag-info.txt/Source-Organization="Faber College" 
  Source-Organization="Faber College"

Note that tag values are quoted in their entirety: both the name and
the value.

Apply double quotes to values containing special characters such as 
spaces and symbols and to values containing environment variables that 
you want to expand, such as "$HOME".

Apply single quotes to values containing symbols that you don't want
the shell to expand, such as curly braces, ampersands, and random dollar 
signs.

You can specify any tag files and tag names you want.

The following example packages the directory /home/josie/photos according
to the APTrust BagIt profile and writes the tarred bag into 
/home/josie/bags/photos.tar.

This bag will include md5 and sha256 manifests and tag manifests. It will
also includ the specified tags in the bag-info.txt and aptrust-info.txt
tag files.

  aptrust bag create \
    --profile=aptrust \
    --manifest-algs="md5,sha256" \
	--bag-dir="/home/josie/photos" \
    --output-dir="/home/josie/bags" \
    --tags="aptrust-info.txt/Title=My Bag of Photos" \
    --tags="aptrust-info.txt/Access=Institution" \ 
    --tags="aptrust-info.txt/Storage-Option=Standard" \ 
    --tags="bag-info.txt/Source-Organization=Faber College" \ 
    --tags='Custom-Tag=Single quoted because it {contains} $weird &characters' 

Limitations:

1. This tool currently supports only APTrust, BTR, and empty/generic 
   BagIt profiles.
2. For now, all bags will be output as tar files.
3. This tool currently supports only the md5, sha1, sha256, and sha512 
   algorithms for manifests and tag manifests.

See also:

  aptrust bag validate --help
	`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(manifestAlgs) == 0 {
			fmt.Println("You must specify at least one manifest algorithm. See `aptrust bag create --help`.")
			os.Exit(EXIT_USER_ERR)
		}
		outputDir := GetFlagValue(cmd.Flags(), "output-dir", "Flag --output-dir is required.")
		profileName := GetFlagValue(cmd.Flags(), "profile", "Flag --profile is required.")
		bagDir := GetFlagValue(cmd.Flags(), "bag-dir", "Flag --bag-dir is required.")
		profile, err := LoadProfile(profileName)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(EXIT_RUNTIME_ERR)
		}

		tags := GetTagValues(userSuppliedTags)
		tags = EnsureDefaultTags(tags)

		logger.Debug("Directory to Bag:   ", bagDir)
		logger.Debug("Output Directory:   ", outputDir)
		logger.Debug("Profile Name:       ", profileName)
		logger.Debug("Profile:            ", profile.Name)
		logger.Debug("Manifest Algorithms:", strings.Join(manifestAlgs, ", "))
		logger.Debug("Tag Values:")
		for _, t := range tags {
			logger.Debug("File:", t.TagFile, "Name:", t.TagName, "Value:", t.GetValue())
		}
		if debug {
			os.Stderr.Sync()
			time.Sleep(2 * time.Second)
		}
		errors := ValidateTags(profile, tags)
		if len(errors) > 0 {
			PrintErrors(errors)
			os.Exit(EXIT_USER_ERR)
		}

		errors = ValidateManifestAlgorithms(profile, manifestAlgs)
		if len(errors) > 0 {
			PrintErrors(errors)
			os.Exit(EXIT_USER_ERR)
		}

		absPath, err := filepath.Abs(bagDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Can't convert", bagDir, "to absolute path.", err.Error())
			os.Exit(EXIT_USER_ERR)
		}

		filestat, err := os.Stat(absPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error accessing", absPath, ":", err.Error())
			os.Exit(EXIT_USER_ERR)
		}
		filesToBag := []*util.ExtendedFileInfo{
			util.NewExtendedFileInfo(absPath, filestat),
		}

		// Apply the user-supplied tag values
		for _, tag := range tags {
			profile.SetTagValue(tag.TagFile, tag.TagName, tag.GetValue())
		}

		// Create the bag
		bagger := bagit.NewBagger(outputDir, profile, filesToBag)
		ok := bagger.Run()
		if !ok {
			for key, value := range bagger.Errors {
				fmt.Fprintln(os.Stderr, key, ":", value)
			}
			os.Exit(EXIT_RUNTIME_ERR)
		}
		fmt.Printf(`{ "result": "OK", "outputPath": "%s" }\n`, bagger.OutputPath)
		os.Exit(EXIT_OK)
	},
}

func init() {
	bagCmd.AddCommand(createCmd)
	createCmd.Flags().StringP("profile", "p", "", "BagIt profile: 'aptrust', 'btr' or 'empty'")
	createCmd.Flags().StringP("bag-dir", "b", "", "Directory containing files you want to package into a bag")
	createCmd.Flags().StringP("output-dir", "o", "", "Output directory. Where should we write the bag?")
	createCmd.Flags().StringSliceVarP(&manifestAlgs, "manifest-algs", "m", []string{""}, "Manifest algorithms. Specify one, or use comma-separated list for multiple. Supported algorithms: md5, sha1, sha256, sha512. Default is sha256.")
	createCmd.Flags().StringSliceVarP(&userSuppliedTags, "tags", "t", []string{""}, "Tag values to write into tag files. You can specify this flag multiple times. See --help for full documentation.")
}

func EnsureDefaultTags(tags []*bagit.TagDefinition) []*bagit.TagDefinition {
	bagitVersion := FindTag(tags, "bagit.txt", "BagIt-Version")
	if bagitVersion == nil {
		versionTag := &bagit.TagDefinition{
			TagFile:   "bagit.txt",
			TagName:   "BagIt-Version",
			UserValue: "1.0",
		}
		tags = append(tags, versionTag)
	} else if bagitVersion.GetValue() == "" {
		bagitVersion.UserValue = "1.0"
	}
	encoding := FindTag(tags, "bagit.txt", "Tag-File-Character-Encoding")
	if encoding == nil {
		encodingTag := &bagit.TagDefinition{
			TagFile:   "bagit.txt",
			TagName:   "Tag-File-Character-Encoding",
			UserValue: "UTF-8",
		}
		tags = append(tags, encodingTag)
	} else if encoding.GetValue() == "" {
		encoding.UserValue = "UTF-8"
	}
	return tags
}

// ValidateTags verifies that tags required by the BagIt profile are
// present and contain valid values. We check this BEFORE bagging because
// in case where the user is packaging 500+ GB, they don't want to wait
// two hours to find out their bag is invalid.
func ValidateTags(profile *bagit.Profile, tags []*bagit.TagDefinition) []string {
	errors := make([]string, 0)
	for _, tagDef := range profile.Tags {
		hasValue := false
		userTag := FindTag(tags, tagDef.TagFile, tagDef.TagName)
		if tagDef.Required && userTag == nil {
			errors = append(errors, fmt.Sprintf("Required tag %s/%s is missing.", tagDef.TagFile, tagDef.TagName))
			continue
		}
		if userTag != nil && userTag.UserValue != "" {
			hasValue = true
		}
		if userTag != nil && !tagDef.IsLegalValue(userTag.UserValue) {
			errors = append(errors, fmt.Sprintf("Tag %s/%s assigned illegal value '%s'. Valid values are: %s.", tagDef.TagFile, tagDef.TagName, userTag.UserValue, strings.Join(tagDef.Values, ",")))
			continue
		}
		if tagDef.Required && !tagDef.EmptyOK && !hasValue {
			errors = append(errors, fmt.Sprintf("Tag %s/%s is present but value cannot be empty. Please assign a value.", tagDef.TagFile, tagDef.TagName))
		}
	}
	return errors
}

// ValidateManifestAlgorithms checks to see whether the user-specified manifest
// algorithms are allowed by the profile, and whether the user specified all
// of the profile's required algorithms. We do this work up front, before creating
// the bag, to avoid creating an invalid bag.
func ValidateManifestAlgorithms(profile *bagit.Profile, algs []string) []string {
	errors := make([]string, 0)
	for _, alg := range algs {
		isAllowed := false
		for _, allowedAlg := range profile.ManifestsAllowed {
			if allowedAlg == alg {
				isAllowed = true
			}
		}
		if !isAllowed {
			errors = append(errors, fmt.Sprintf("Manifest algorithm '%s' is not allowed in profile %s.", alg, profile.Name))
		}
	}
	for _, requiredAlg := range profile.ManifestsRequired {
		foundRequiredAlg := false
		for _, alg := range algs {
			if alg == requiredAlg {
				foundRequiredAlg = true
			}
		}
		if !foundRequiredAlg {
			errors = append(errors, fmt.Sprintf("Profile %s requires manifest algorithm %s", profile.Name, requiredAlg))
		}
	}
	return errors
}

// TODO: Change this to find tags? Tags can repeat.
func FindTag(tags []*bagit.TagDefinition, tagFile, tagName string) *bagit.TagDefinition {
	for _, tag := range tags {
		if tag.TagFile == tagFile && tag.TagName == tagName {
			return tag
		}
	}
	return nil
}
