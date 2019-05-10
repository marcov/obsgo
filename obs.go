package obsgo

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	pb "gopkg.in/cheggaaa/pb.v1"
)

type Project struct {
	Name     string
	User     string
	Password string
}

type PackageInfo struct {
	Name  string
	Path  string
	Repo  string
	Arch  string
	Files []PkgBinary
}

// Given a PackageInfo instance, returns all binary Package files published
// on the OBS project, whose names match the binaryPackageRE regular expression.
func (proj *Project) PackageBinaries(pkg *PackageInfo) error {
	binaryPackageRE := fmt.Sprintf(`(_(all|%s)\.deb$|\.(noarch|%s)\.rpm)$`, pkg.Arch, pkg.Arch)

	pkg.Path = path.Join(pkg.Repo, pkg.Arch, pkg.Name)

	logrus.Debugf("Retrieving binaries for %s", pkg.Path)

	allBins, err := proj.listBinaries(pkg.Path)
	if err != nil {
		return errors.Wrapf(err, "Failed to get get list of OBS binaries")
	}

	re := regexp.MustCompile(binaryPackageRE)

	for _, b := range allBins {
		logrus.WithField("file", b).Debug("processing")
		if re.Match([]byte(b.Filename)) {
			pkg.Files = append(pkg.Files, b)
		}
	}

	return nil
}

// Returns all the packages files published on the OBS project.
func (proj *Project) FindAllPackages() ([]PackageInfo, error) {
	var pkgList []PackageInfo

	logrus.WithField("project", proj.Name).Debug("Finding all package files")

	progressBar := pb.New(0)
	progressBar.SetMaxWidth(100)
	progressBar.Start()
	defer progressBar.Finish()

	repos, err := proj.ListRepos()
	if err != nil {
		return pkgList, errors.Wrapf(err, "failed to get list of repos for project %s\n", proj.Name)
	}

	for _, repo := range repos {
		archs, err := proj.ListArchs(repo)
		if err != nil {
			return pkgList, errors.Wrapf(err, "failed to get list of archs for project %s\n", proj.Name)
		}

		for _, arch := range archs {
			pkgs, err := proj.ListPackages(repo, arch)
			if err != nil {
				return pkgList, errors.Wrapf(err, "failed to get list of pkgs for project %s\n", proj.Name)
			}

			for _, pkg := range pkgs {
				if progressBar.Get() == 0 {
					progressBar.SetTotal(len(repos) * len(pkgs) * len(archs))
				}

				progressBar.Increment()

				newPkg := PackageInfo{
					Name: pkg,
					Repo: repo,
					Arch: arch,
				}

				err := proj.PackageBinaries(&newPkg)
				if err != nil {
					return pkgList, err
				}

				pkgList = append(pkgList, newPkg)
			}
		}
	}

	return pkgList, nil
}

// Downloads all the files specified in the passed pkgInfo argument, and returns
// a slice with a list of the locally downloaded files.
func (proj *Project) DownloadPackageFiles(pkgInfo PackageInfo, root string) ([]string, error) {
	var filePaths []string
	logrus.Debugf("Downloading Package files for %s / %s", proj.Name, pkgInfo.Repo)

	progressBar := pb.New(len(pkgInfo.Files))
	progressBar.SetMaxWidth(100)
	progressBar.Start()
	defer progressBar.Finish()

	for _, f := range pkgInfo.Files {
		logrus.Debugf("Downloading %s", f.Filename)

		remotePath := path.Join(pkgInfo.Path, f.Filename)
		localFile := filepath.Join(root, proj.Name, remotePath)
		filePaths = append(filePaths, localFile)

		info, err := os.Stat(localFile)
		if !(err == nil || os.IsNotExist(err)) {
			return filePaths, err
		}

		fsize, err := strconv.Atoi(f.Size)
		if err != nil {
			return filePaths, errors.Wrapf(err, "could not parse file size %s", localFile)
		}

		if info != nil && info.Size() == int64(fsize) {
			logrus.Debugf("File already downloaded")
			progressBar.Increment()
			continue
		}

		err = os.MkdirAll(filepath.Dir(localFile), 0700)
		if err != nil {
			return filePaths, errors.Wrapf(err, "could not mkdir path %s", remotePath)
		}

		destFile, err := os.Create(localFile)
		if err != nil {
			return filePaths, errors.Wrapf(err, "could not create local file %s", localFile)
		}

		err = proj.downloadBinary(remotePath, destFile)
		if err != nil {
			return filePaths, errors.Wrapf(err, "could not download binary at %s", remotePath)
		}

		progressBar.Increment()
	}

	return filePaths, nil
}

func (proj *Project) ListRepos() ([]string, error) {
	return proj.listDirectories("")
}

func (proj *Project) ListArchs(repo string) ([]string, error) {
	return proj.listDirectories(repo)
}

func (proj *Project) ListPackages(repo, arch string) ([]string, error) {
	url := path.Join(repo, arch)
	return proj.listDirectories(url)
}
