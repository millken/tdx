package tdx

import (
	"encoding/binary"
	"fmt"
	"math"
	"time"
)

// FinanceInfo represents fundamental financial data for a stock.
//
// Wire format (opentdx): "<HB6sfHHII" + 31 "f" = 9 (header) + 140 (data) bytes.
// All float fields are IEEE 754 little-endian float32, converted to float64.
type FinanceInfo struct {
	LiuTongGuBen float64 // 流通股本(股)
	Province     uint16  // 省份代码
	Industry     uint16  // 行业代码
	UpdatedDate  uint32  // 更新日期 YYYYMMDD
	IPODate      uint32  // 上市日期 YYYYMMDD

	ZongGuBen      float64 // 总股本(股)
	GuoJiaGu       float64 // 国家股(股)
	FaQiRenFaRenGu float64 // 发起人法人股(股)
	FaRenGu        float64 // 法人股(股)
	BGu            float64 // B股(股)
	HGu            float64 // H股(股)

	MeiGuShouYi       float64 // 每股收益(元)
	ZiChanZongJi      float64 // 资产总计(元)
	LiuDongZiChanZongJi float64 // 流动资产总计(元)
	GuDingZiChanJinE  float64 // 固定资产金额(元)
	WuXingZiChan      float64 // 无形资产(元)
	GuDongRenShu      float64 // 股东人数

	LiuDongFuZhaiHeJi float64 // 流动负债合计(元)
	ChangQiFuZhai     float64 // 长期负债(元)
	ZiBenGongJiJin    float64 // 资本公积金(元)
	GuiMoQuanYiHeJi   float64 // 所有者权益(元)

	YinYeZongShouRu  float64 // 营业总收入(元)
	YinYeChengBen    float64 // 营业成本(元)
	YingShouZhangKuan float64 // 应收帐款(元)
	YinYeLiRun       float64 // 营业利润(元)
	TouZiShouYi      float64 // 投资收益(元)

	JingYingXianJinLiu float64 // 经营现金流量净额(元)
	ZongXianJinLiu     float64 // 总现金流(元)
	CunHuo             float64 // 存货(元)
	LiRunZongE         float64 // 利润总额(元)
	ShuiHouLiRun       float64 // 税后利润(元)

	GuiMoJinLiRun  float64 // 净利润(元)
	WeiFenLiRun    float64 // 未分配利润(元)
	MeiGuJingZiChan float64 // 每股净资产(元)
	BaoLiu2        float64 // 保留字段
}

// RequestFinanceFrame builds a 0x0010 finance info request frame.
func RequestFinanceFrame(msgID uint32, control byte, market uint16, code string) ([]byte, error) {
	body := make([]byte, 9)
	binary.LittleEndian.PutUint16(body[0:2], 1)
	body[2] = byte(market)
	copy(body[3:9], code)
	return BuildDirectFrame(msgID, control, DirectFrameTypeFinance, body), nil
}

// GetFinance retrieves fundamental financial data for the given stock code.
func (c *Client) GetFinance(code string) (*FinanceInfo, error) {
	if err := c.ensureConn(); err != nil {
		return nil, err
	}

	normalizedCode, market, hasPrefixedMarket, err := normalizeKlineCode(code)
	if err != nil {
		return nil, err
	}
	if !hasPrefixedMarket {
		market, err = inferKlineMarket(normalizedCode)
		if err != nil {
			return nil, err
		}
	}

	c.drainPending()

	packet, err := RequestFinanceFrame(0x000A0401, 0x01, market, normalizedCode)
	if err != nil {
		return nil, err
	}
	if err := c.sendRaw(packet); err != nil {
		return nil, err
	}

	response, err := c.waitForCMD(DirectFrameTypeFinance, 8*time.Second)
	if err != nil {
		return nil, err
	}

	if response == nil || response.Body == nil {
		return nil, fmt.Errorf("tdx: empty finance response")
	}

	return DecodeFinance(response.Body.Decoded)
}

// DecodeFinance decodes a 0x0010 finance response body.
func DecodeFinance(body []byte) (*FinanceInfo, error) {
	const headerSize = 9  // H(uint16) + B(uint8) + 6s
	const dataSize = 136  // fHHII + 30f = 4+2+2+4+4+120
	if len(body) < headerSize+dataSize {
		return nil, fmt.Errorf("finance body too short: %d", len(body))
	}

	bs := body[headerSize : headerSize+dataSize]
	info := &FinanceInfo{}

	off := 0
	info.LiuTongGuBen = float64(readFloat32(bs[off:])); off += 4
	info.Province = binary.LittleEndian.Uint16(bs[off:]); off += 2
	info.Industry = binary.LittleEndian.Uint16(bs[off:]); off += 2
	info.UpdatedDate = binary.LittleEndian.Uint32(bs[off:]); off += 4
	info.IPODate = binary.LittleEndian.Uint32(bs[off:]); off += 4

	info.ZongGuBen = float64(readFloat32(bs[off:])); off += 4
	info.GuoJiaGu = float64(readFloat32(bs[off:])); off += 4
	info.FaQiRenFaRenGu = float64(readFloat32(bs[off:])); off += 4
	info.FaRenGu = float64(readFloat32(bs[off:])); off += 4
	info.BGu = float64(readFloat32(bs[off:])); off += 4
	info.HGu = float64(readFloat32(bs[off:])); off += 4

	info.MeiGuShouYi = float64(readFloat32(bs[off:])); off += 4
	info.ZiChanZongJi = float64(readFloat32(bs[off:])); off += 4
	info.LiuDongZiChanZongJi = float64(readFloat32(bs[off:])); off += 4
	info.GuDingZiChanJinE = float64(readFloat32(bs[off:])); off += 4
	info.WuXingZiChan = float64(readFloat32(bs[off:])); off += 4
	info.GuDongRenShu = float64(readFloat32(bs[off:])); off += 4

	info.LiuDongFuZhaiHeJi = float64(readFloat32(bs[off:])); off += 4
	info.ChangQiFuZhai = float64(readFloat32(bs[off:])); off += 4
	info.ZiBenGongJiJin = float64(readFloat32(bs[off:])); off += 4
	info.GuiMoQuanYiHeJi = float64(readFloat32(bs[off:])); off += 4

	info.YinYeZongShouRu = float64(readFloat32(bs[off:])); off += 4
	info.YinYeChengBen = float64(readFloat32(bs[off:])); off += 4
	info.YingShouZhangKuan = float64(readFloat32(bs[off:])); off += 4
	info.YinYeLiRun = float64(readFloat32(bs[off:])); off += 4
	info.TouZiShouYi = float64(readFloat32(bs[off:])); off += 4

	info.JingYingXianJinLiu = float64(readFloat32(bs[off:])); off += 4
	info.ZongXianJinLiu = float64(readFloat32(bs[off:])); off += 4
	info.CunHuo = float64(readFloat32(bs[off:])); off += 4
	info.LiRunZongE = float64(readFloat32(bs[off:])); off += 4
	info.ShuiHouLiRun = float64(readFloat32(bs[off:])); off += 4

	info.GuiMoJinLiRun = float64(readFloat32(bs[off:])); off += 4
	info.WeiFenLiRun = float64(readFloat32(bs[off:])); off += 4
	info.MeiGuJingZiChan = float64(readFloat32(bs[off:])); off += 4
	info.BaoLiu2 = float64(readFloat32(bs[off:])); off += 4

	return info, nil
}

func readFloat32(bs []byte) float32 {
	return math.Float32frombits(binary.LittleEndian.Uint32(bs[:4]))
}
