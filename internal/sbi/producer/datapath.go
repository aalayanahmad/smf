package producer

import (
	"fmt"
	"time"

	"github.com/aalayanahmad/pfcp"
	"github.com/aalayanahmad/pfcp/pfcpType"
	"github.com/aalayanahmad/pfcp/pfcpUdp"
	smf_context "github.com/aalayanahmad/smf/internal/context"
	"github.com/aalayanahmad/smf/internal/logger"
	pfcp_message "github.com/aalayanahmad/smf/internal/pfcp/message"
	"github.com/free5gc/nas/nasMessage"
	"github.com/free5gc/openapi/models"
)

type PFCPState struct {
	upf     *smf_context.UPF
	pdrList []*smf_context.PDR
	farList []*smf_context.FAR
	barList []*smf_context.BAR
	qerList []*smf_context.QER
	urrList []*smf_context.URR
	srrList []*smf_context.SRR
}

type SendPfcpResult struct {
	Status smf_context.PFCPSessionResponseStatus
	RcvMsg *pfcpUdp.Message
	Err    error
}

// ActivateUPFSession send all datapaths to UPFs and send result to UE
// It returns after all PFCP response have been returned or timed out,
// and before sending N1N2MessageTransfer request if it is needed.
func ActivateUPFSession(
	smContext *smf_context.SMContext,
	notifyUeHander func(*smf_context.SMContext, bool),
) {
	pfcpPool := make(map[string]*PFCPState)

	for _, dataPath := range smContext.Tunnel.DataPathPool {
		if !dataPath.Activated {
			continue
		}
		for node := dataPath.FirstDPNode; node != nil; node = node.Next() {
			pdrList := make([]*smf_context.PDR, 0, 2)
			farList := make([]*smf_context.FAR, 0, 2)
			qerList := make([]*smf_context.QER, 0, 2)
			urrList := make([]*smf_context.URR, 0, 2)
			srrList := make([]*smf_context.SRR, 0, 2)

			if node.UpLinkTunnel != nil && node.UpLinkTunnel.PDR != nil {
				pdrList = append(pdrList, node.UpLinkTunnel.PDR)
				farList = append(farList, node.UpLinkTunnel.PDR.FAR)
				if node.UpLinkTunnel.PDR.QER != nil {
					qerList = append(qerList, node.UpLinkTunnel.PDR.QER...)
				}
				if node.UpLinkTunnel.PDR.URR != nil {
					urrList = append(urrList, node.UpLinkTunnel.PDR.URR...)
				}
				if node.UpLinkTunnel.PDR.SRR != nil {
					srrList = append(srrList, node.UpLinkTunnel.PDR.SRR...)
				}
				// Define the new SRR struct with predefined values
				var BASE_DATE_NTP_ERA0 = time.Date(1900, time.January, 1, 0, 0, 0, 0, time.UTC)
				duration_200ms := 1000 * time.Millisecond // 500 ms duration
				newSRR := &smf_context.SRR{
					SRRID: 1,
					QoSMonitoringPerQoSFlowControlInformation: []*smf_context.QoSMonitoringPerQoSFlowControlInformation{
						{
							QFI: 6,
							RequestedQoSMonitoring: &pfcpType.RequestedQosMonitoring{
								DLPD:   false,
								ULPD:   true,
								RPPD:   false,
								GTPUPM: false,
								DLCI:   false,
								ULCI:   false,
								DLDR:   false,
								ULDR:   false,
							},
							ReportingFrequency: &pfcpType.ReportingFrequency{
								RESERVED: false,
								PERIO:    false,
								EVET:     true,
							},
							PacketDelayThresholds: &pfcpType.PacketDelayThresholds{
								DL:                        false,
								UL:                        true,
								RP:                        false,
								UpPacketDelayThresholdRID: 300,
							},
							MinimumWaitTime: &pfcpType.MinimumWaitTime{
								MinimumWaitTime: BASE_DATE_NTP_ERA0.Add(duration_200ms),
							},
							MeasurementPeriod: &pfcpType.MeasurementPeriod{
								MeasurementPeriod: 1,
							},
						},
					},
					State: smf_context.RULE_INITIAL,
				}

				// Append the new SRR struct to srrList
				srrList = append(srrList, newSRR)

			}
			if node.DownLinkTunnel != nil && node.DownLinkTunnel.PDR != nil {
				pdrList = append(pdrList, node.DownLinkTunnel.PDR)
				farList = append(farList, node.DownLinkTunnel.PDR.FAR)
				// skip send QER because uplink and downlink shared one QER
			}

			pfcpState := pfcpPool[node.GetNodeIP()]
			if pfcpState == nil {
				pfcpPool[node.GetNodeIP()] = &PFCPState{
					upf:     node.UPF,
					pdrList: pdrList,
					farList: farList,
					qerList: qerList,
					urrList: urrList,
					srrList: srrList,
				}
			} else {
				pfcpState.pdrList = append(pfcpState.pdrList, pdrList...)
				pfcpState.farList = append(pfcpState.farList, farList...)
				pfcpState.qerList = append(pfcpState.qerList, qerList...)
				pfcpState.urrList = append(pfcpState.urrList, urrList...)
				pfcpState.srrList = append(pfcpState.srrList, srrList...)
			}
		}
	}

	resChan := make(chan SendPfcpResult)

	for ip, pfcp := range pfcpPool {
		sessionContext, exist := smContext.PFCPContext[ip]
		if !exist || sessionContext.RemoteSEID == 0 {
			go establishPfcpSession(smContext, pfcp, resChan)
		} else {
			go modifyExistingPfcpSession(smContext, pfcp, resChan, "")
		}
	}

	waitAllPfcpRsp(smContext, len(pfcpPool), resChan, notifyUeHander)
	close(resChan)
}

func QueryReport(smContext *smf_context.SMContext, upf *smf_context.UPF,
	urrs []*smf_context.URR, reportResaon models.TriggerType,
) {
	for _, urr := range urrs {
		urr.State = smf_context.RULE_QUERY
	}

	pfcpState := &PFCPState{
		upf:     upf,
		urrList: urrs,
	}

	resChan := make(chan SendPfcpResult)
	go modifyExistingPfcpSession(smContext, pfcpState, resChan, reportResaon)
	pfcpResult := <-resChan

	if pfcpResult.Err != nil {
		logger.PduSessLog.Errorf("Query URR Report by PFCP Session Mod Request fail: %v", pfcpResult.Err)
		return
	}
}

func establishPfcpSession(smContext *smf_context.SMContext,
	state *PFCPState,
	resCh chan<- SendPfcpResult,
) {
	logger.PduSessLog.Infoln("Sending PFCP Session Establishment Request - - ahmad modified")

	rcvMsg, err := pfcp_message.SendPfcpSessionEstablishmentRequest(
		state.upf, smContext, state.pdrList, state.farList, state.barList, state.qerList, state.urrList, state.srrList)
	if err != nil {
		logger.PduSessLog.Warnf("Sending PFCP Session Establishment Request error - - ahmad modified: %+v", err)
		resCh <- SendPfcpResult{
			Status: smf_context.SessionEstablishFailed,
			Err:    err,
		}
		return
	}

	rsp := rcvMsg.PfcpMessage.Body.(pfcp.PFCPSessionEstablishmentResponse)
	if rsp.UPFSEID != nil {
		NodeIDtoIP := rsp.NodeID.ResolveNodeIdToIp().String()
		pfcpSessionCtx := smContext.PFCPContext[NodeIDtoIP]
		pfcpSessionCtx.RemoteSEID = rsp.UPFSEID.Seid
	}

	if rsp.Cause != nil && rsp.Cause.CauseValue == pfcpType.CauseRequestAccepted {
		logger.PduSessLog.Infoln("Received PFCP Session Establishment Accepted Response - - ahmad modified")
		resCh <- SendPfcpResult{
			Status: smf_context.SessionEstablishSuccess,
			RcvMsg: rcvMsg,
		}
	} else {
		logger.PduSessLog.Infoln("Received PFCP Session Establishment Not Accepted Response - - ahmad modified")
		resCh <- SendPfcpResult{
			Status: smf_context.SessionEstablishFailed,
			Err:    fmt.Errorf("cause[%d] if not request accepted", rsp.Cause.CauseValue),
		}
	}
}

func modifyExistingPfcpSession(
	smContext *smf_context.SMContext,
	state *PFCPState,
	resCh chan<- SendPfcpResult,
	reportResaon models.TriggerType,
) {
	logger.PduSessLog.Infoln("Sending PFCP Session Modification Request")

	rcvMsg, err := pfcp_message.SendPfcpSessionModificationRequest(
		state.upf, smContext, state.pdrList, state.farList, state.barList, state.qerList, state.urrList)
	if err != nil {
		logger.PduSessLog.Warnf("Sending PFCP Session Modification Request error: %+v", err)
		resCh <- SendPfcpResult{
			Status: smf_context.SessionUpdateFailed,
			Err:    err,
		}
		return
	}

	logger.PduSessLog.Infoln("Received PFCP Session Modification Response")

	rsp := rcvMsg.PfcpMessage.Body.(pfcp.PFCPSessionModificationResponse)
	if rsp.Cause != nil && rsp.Cause.CauseValue == pfcpType.CauseRequestAccepted {
		resCh <- SendPfcpResult{
			Status: smf_context.SessionUpdateSuccess,
			RcvMsg: rcvMsg,
		}
		if rsp.UsageReport != nil {
			SEID := rcvMsg.PfcpMessage.Header.SEID
			upfNodeID := smContext.GetNodeIDByLocalSEID(SEID)
			smContext.HandleReports(nil, rsp.UsageReport, nil, upfNodeID, reportResaon)
		}
	} else {
		resCh <- SendPfcpResult{
			Status: smf_context.SessionUpdateFailed,
			Err:    fmt.Errorf("cause[%d] if not request accepted", rsp.Cause.CauseValue),
		}
	}
}

func waitAllPfcpRsp(
	smContext *smf_context.SMContext,
	pfcpPoolLen int,
	resChan <-chan SendPfcpResult,
	notifyUeHander func(*smf_context.SMContext, bool),
) {
	success := true
	for i := 0; i < pfcpPoolLen; i++ {
		res := <-resChan
		if notifyUeHander == nil {
			continue
		}

		if res.Status == smf_context.SessionEstablishFailed ||
			res.Status == smf_context.SessionUpdateFailed {
			success = false
		}
	}
	if notifyUeHander != nil {
		notifyUeHander(smContext, success)
	}
}

func EstHandler(isDone <-chan struct{},
	smContext *smf_context.SMContext, success bool,
) {
	// Waiting for Create SMContext Request completed
	if isDone != nil {
		<-isDone
	}
	if success {
		sendPDUSessionEstablishmentAccept(smContext)
	} else {
		// TODO: set appropriate 5GSM cause according to PFCP cause value
		sendPDUSessionEstablishmentReject(smContext, nasMessage.Cause5GSMNetworkFailure)
	}
}

func ModHandler(smContext *smf_context.SMContext, success bool) {
}

func sendPDUSessionEstablishmentReject(
	smContext *smf_context.SMContext,
	nasErrorCause uint8,
) {
	smNasBuf, err := smf_context.BuildGSMPDUSessionEstablishmentReject(
		smContext, nasMessage.Cause5GSMNetworkFailure)
	if err != nil {
		logger.PduSessLog.Errorf("Build GSM PDUSessionEstablishmentReject failed: %s", err)
		return
	}

	n1n2Request := models.N1N2MessageTransferRequest{
		BinaryDataN1Message: smNasBuf,
		JsonData: &models.N1N2MessageTransferReqData{
			PduSessionId: smContext.PDUSessionID,
			N1MessageContainer: &models.N1MessageContainer{
				N1MessageClass:   "SM",
				N1MessageContent: &models.RefToBinaryData{ContentId: "GSM_NAS"},
			},
		},
	}

	ctx, _, err := smf_context.GetSelf().GetTokenCtx(models.ServiceName_NAMF_COMM, models.NfType_AMF)
	if err != nil {
		logger.PduSessLog.Warnf("Get NAMF_COMM context failed: %s", err)
		return
	}

	rspData, rsp, err := smContext.
		CommunicationClient.
		N1N2MessageCollectionDocumentApi.
		N1N2MessageTransfer(ctx, smContext.Supi, n1n2Request)
	defer func() {
		if rsp != nil {
			if resCloseErr := rsp.Body.Close(); resCloseErr != nil {
				logger.PduSessLog.Warnf("response Body closed error")
			}
		}
	}()
	smContext.SetState(smf_context.InActive)
	if err != nil {
		logger.PduSessLog.Warnf("Send N1N2Transfer failed")
		return
	}
	if rspData.Cause == models.N1N2MessageTransferCause_N1_MSG_NOT_TRANSFERRED {
		logger.PduSessLog.Warnf("%v", rspData.Cause)
	}
	RemoveSMContextFromAllNF(smContext, true)
}

func sendPDUSessionEstablishmentAccept(
	smContext *smf_context.SMContext,
) {
	smNasBuf, err := smf_context.BuildGSMPDUSessionEstablishmentAccept(smContext)
	if err != nil {
		logger.PduSessLog.Errorf("Build GSM PDUSessionEstablishmentAccept failed: %s", err)
		return
	}

	n2Pdu, err := smf_context.BuildPDUSessionResourceSetupRequestTransfer(smContext)
	if err != nil {
		logger.PduSessLog.Errorf("Build PDUSessionResourceSetupRequestTransfer failed: %s", err)
		return
	}

	n1n2Request := models.N1N2MessageTransferRequest{
		BinaryDataN1Message:     smNasBuf,
		BinaryDataN2Information: n2Pdu,
		JsonData: &models.N1N2MessageTransferReqData{
			PduSessionId: smContext.PDUSessionID,
			N1MessageContainer: &models.N1MessageContainer{
				N1MessageClass:   "SM",
				N1MessageContent: &models.RefToBinaryData{ContentId: "GSM_NAS"},
			},
			N2InfoContainer: &models.N2InfoContainer{
				N2InformationClass: models.N2InformationClass_SM,
				SmInfo: &models.N2SmInformation{
					PduSessionId: smContext.PDUSessionID,
					N2InfoContent: &models.N2InfoContent{
						NgapIeType: models.NgapIeType_PDU_RES_SETUP_REQ,
						NgapData: &models.RefToBinaryData{
							ContentId: "N2SmInformation",
						},
					},
					SNssai: smContext.SNssai,
				},
			},
		},
	}

	ctx, _, err := smf_context.GetSelf().GetTokenCtx(models.ServiceName_NAMF_COMM, models.NfType_AMF)
	if err != nil {
		logger.PduSessLog.Warnf("Get NAMF_COMM context failed: %s", err)
		return
	}

	rspData, rsp, err := smContext.
		CommunicationClient.
		N1N2MessageCollectionDocumentApi.
		N1N2MessageTransfer(ctx, smContext.Supi, n1n2Request)
	defer func() {
		if rsp != nil {
			if resCloseErr := rsp.Body.Close(); resCloseErr != nil {
				logger.PduSessLog.Warnf("response Body closed error")
			}
		}
	}()
	smContext.SetState(smf_context.Active)

	if err != nil {
		logger.PduSessLog.Warnf("Send N1N2Transfer failed")
		return
	}
	if rspData.Cause == models.N1N2MessageTransferCause_N1_MSG_NOT_TRANSFERRED {
		logger.PduSessLog.Warnf("%v", rspData.Cause)
	}
}

func updateAnUpfPfcpSession(
	smContext *smf_context.SMContext,
	pdrList []*smf_context.PDR,
	farList []*smf_context.FAR,
	barList []*smf_context.BAR,
	qerList []*smf_context.QER,
	urrList []*smf_context.URR,
) smf_context.PFCPSessionResponseStatus {
	defaultPath := smContext.Tunnel.DataPathPool.GetDefaultPath()
	ANUPF := defaultPath.FirstDPNode
	rcvMsg, err := pfcp_message.SendPfcpSessionModificationRequest(
		ANUPF.UPF, smContext, pdrList, farList, barList, qerList, urrList)
	if err != nil {
		logger.PduSessLog.Warnf("Sending PFCP Session Modification Request to AN UPF error: %+v", err)
		return smf_context.SessionUpdateFailed
	}

	rsp := rcvMsg.PfcpMessage.Body.(pfcp.PFCPSessionModificationResponse)
	if rsp.Cause == nil || rsp.Cause.CauseValue != pfcpType.CauseRequestAccepted {
		logger.PduSessLog.Warn("Received PFCP Session Modification Not Accepted Response from AN UPF")
		return smf_context.SessionUpdateFailed
	}

	logger.PduSessLog.Info("Received PFCP Session Modification Accepted Response from AN UPF")

	if smf_context.GetSelf().ULCLSupport && smContext.BPManager != nil {
		if smContext.BPManager.BPStatus == smf_context.UnInitialized {
			logger.PfcpLog.Infoln("Add PSAAndULCL")
			// TODO: handle error cases
			AddPDUSessionAnchorAndULCL(smContext)
			smContext.BPManager.BPStatus = smf_context.AddingPSA
		}
	}

	return smf_context.SessionUpdateSuccess
}

func ReleaseTunnel(smContext *smf_context.SMContext) []SendPfcpResult {
	resChan := make(chan SendPfcpResult)

	deletedPFCPNode := make(map[string]bool)
	for _, dataPath := range smContext.Tunnel.DataPathPool {
		var targetNodes []*smf_context.DataPathNode
		for node := dataPath.FirstDPNode; node != nil; node = node.Next() {
			targetNodes = append(targetNodes, node)
		}
		dataPath.DeactivateTunnelAndPDR(smContext)
		for _, node := range targetNodes {
			curUPFID, err := node.GetUPFID()
			if err != nil {
				logger.PduSessLog.Error(err)
				continue
			}
			if _, exist := deletedPFCPNode[curUPFID]; !exist {
				go deletePfcpSession(node.UPF, smContext, resChan)
				deletedPFCPNode[curUPFID] = true
			}
		}
	}

	// collect all responses
	resList := make([]SendPfcpResult, 0, len(deletedPFCPNode))
	for i := 0; i < len(deletedPFCPNode); i++ {
		resList = append(resList, <-resChan)
	}

	return resList
}

func deletePfcpSession(upf *smf_context.UPF, ctx *smf_context.SMContext, resCh chan<- SendPfcpResult) {
	logger.PduSessLog.Infoln("Sending PFCP Session Deletion Request")

	rcvMsg, err := pfcp_message.SendPfcpSessionDeletionRequest(upf, ctx)
	if err != nil {
		logger.PduSessLog.Warnf("Sending PFCP Session Deletion Request error: %+v", err)
		resCh <- SendPfcpResult{
			Status: smf_context.SessionReleaseFailed,
			Err:    err,
		}
		return
	}

	rsp := rcvMsg.PfcpMessage.Body.(pfcp.PFCPSessionDeletionResponse)
	if rsp.Cause != nil && rsp.Cause.CauseValue == pfcpType.CauseRequestAccepted {
		logger.PduSessLog.Info("Received PFCP Session Deletion Accepted Response")
		resCh <- SendPfcpResult{
			Status: smf_context.SessionReleaseSuccess,
		}
		if rsp.UsageReport != nil {
			SEID := rcvMsg.PfcpMessage.Header.SEID
			upfNodeID := ctx.GetNodeIDByLocalSEID(SEID)
			ctx.HandleReports(nil, nil, rsp.UsageReport, upfNodeID, "")
		}
	} else {
		logger.PduSessLog.Warn("Received PFCP Session Deletion Not Accepted Response")
		resCh <- SendPfcpResult{
			Status: smf_context.SessionReleaseFailed,
			Err:    fmt.Errorf("cause[%d] if not request accepted", rsp.Cause.CauseValue),
		}
	}
}
