package vdbench

import (
	"fmt"
	"strings"

	"github.com/red-hat-storage/odf-io-stress/pkg/config"
)

const filesystemAnchor = "/mnt/data"

func BuildBlockParam(block config.VDBenchBlock, p config.VDBenchPattern, lun string, runtime int) string {
	sd := fmt.Sprintf("sd=sd1,lun=%s,openflags=o_direct,size=%s", lun, block.Size)
	wd := fmt.Sprintf("wd=wd1,sd=sd1,rdpct=%d,seekpct=%d,xfersize=%s,skew=%d",
		p.Rdpct, p.Seekpct, p.Xfersize, p.Skew)
	// Block RD requires iorate= (vdbench rejects RD without it).
	rd := fmt.Sprintf("rd=rd1,wd=wd1,iorate=max,elapsed=%d,interval=1", runtime)
	return sd + "\n" + wd + "\n" + rd + "\n"
}

// fileIOMode maps config seekpct onto FWD fileio/fileselect.
// FWD does not accept seekpct= (that keyword is WD-only); 0 → sequential, else random.
func fileIOMode(seekpct int) string {
	if seekpct == 0 {
		return "sequential"
	}
	return "random"
}

func buildFilesystemFWD(p config.VDBenchPattern) string {
	mode := fileIOMode(p.Seekpct)
	if mode == "sequential" {
		// vdbench50407: fileio=sequential and rdpct= are mutually exclusive.
		// Use operation= (or operations=) instead.
		switch {
		case p.Rdpct <= 0:
			return fmt.Sprintf("fwd=fwd1,fsd=fsd1,xfersize=%s,skew=%d,fileio=sequential,fileselect=sequential,operation=write",
				p.Xfersize, p.Skew)
		case p.Rdpct >= 100:
			return fmt.Sprintf("fwd=fwd1,fsd=fsd1,xfersize=%s,skew=%d,fileio=sequential,fileselect=sequential,operation=read",
				p.Xfersize, p.Skew)
		default:
			return fmt.Sprintf("fwd=fwd1,fsd=fsd1,xfersize=%s,skew=%d,fileio=sequential,fileselect=sequential,operations=(read,write)",
				p.Xfersize, p.Skew)
		}
	}
	return fmt.Sprintf("fwd=fwd1,fsd=fsd1,rdpct=%d,xfersize=%s,skew=%d,fileio=random,fileselect=random",
		p.Rdpct, p.Xfersize, p.Skew)
}

func BuildFilesystemParam(fs config.VDBenchFilesystem, p config.VDBenchPattern, runtime int) string {
	fsd := fmt.Sprintf("fsd=fsd1,anchor=%s,depth=%d,width=%d,files=%d,size=%s",
		filesystemAnchor, fs.Depth, fs.Width, fs.Files, fs.FileSize)
	if fs.OpenFlags != "" {
		fsd += ",openflags=" + fs.OpenFlags
	}
	fwd := buildFilesystemFWD(p)
	// Filesystem RD uses fwdrate= (not iorate=); format=yes creates the file tree first.
	// group_all_fwds_in_one_rd is not valid in vdbench50407 — config field is ignored.
	rd := fmt.Sprintf("rd=rd1,fwd=fwd1,fwdrate=max,format=yes,elapsed=%d,interval=1", runtime)
	var b strings.Builder
	b.WriteString(fsd)
	b.WriteByte('\n')
	b.WriteString(fwd)
	b.WriteByte('\n')
	b.WriteString(rd)
	b.WriteByte('\n')
	return b.String()
}
