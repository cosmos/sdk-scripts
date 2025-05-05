package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	distributiontypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
)

type validatorRewards map[string]string
type chainValCoins map[string]validatorRewards

type chainInfo struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
}

const (
	validatorsEndpoint = "/cosmos/staking/v1beta1/validators"

	outstandingRewardsEndpoint = "/cosmos/distribution/v1beta1/validators/%s/outstanding_rewards"

	ibcDenomsEndpoint = "/ibc/apps/transfer/v1/denoms/%s"
)

func main() {
	bz, err := os.ReadFile("chains.json")
	if err != nil {
		panic(err)
	}

	chains := make([]chainInfo, 0)
	err = json.Unmarshal(bz, &chains)
	if err != nil {
		panic(err)
	}

	cvc := make(chainValCoins)
	cvcSyncMap := sync.Map{}
	wg := sync.WaitGroup{}
	for _, chain := range chains {
		wg.Add(1)
		go func() {
			rewards := make(validatorRewards)
			vals := getValidators(chain.Addr)
			for _, val := range vals {
				rw := getOutstandingRewards(chain.Addr, val)
				for _, r := range rw {
					denom := r.Denom
					if strings.HasPrefix(r.Denom, "ibc/") {
						if newDenom := getDenom(chain.Addr, r.Denom); newDenom != "" {
							denom = newDenom
						}
					}
					rewards[denom] = r.Amount.String()
				}
			}
			cvcSyncMap.Store(chain.Name, rewards)
			wg.Done()
		}()
	}
	wg.Wait()

	cvcSyncMap.Range(func(key, value any) bool {
		k := key.(string)
		v := value.(validatorRewards)
		cvc[k] = v
		return true
	})

	bz, err = json.Marshal(cvc)
	if err != nil {
		panic(err)
	}
	os.WriteFile("validator_rewards.json", bz, 0644)
}

type Validator struct {
	OperatorAddress string `json:"operator_address"`
}

type QueryValidatorsResponseTemp struct {
	Validators []Validator `json:"validators"`
}

func getValidators(addr string) []string {
	res, err := http.Get(addr + validatorsEndpoint)
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()

	bz, err := io.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}

	// Unmarshal into temp struct with string status
	var tempResponse QueryValidatorsResponseTemp
	err = json.Unmarshal(bz, &tempResponse)
	if err != nil {
		panic(err)
	}

	// Now we can access the operator addresses
	addrs := make([]string, 0, len(tempResponse.Validators))
	for _, v := range tempResponse.Validators {
		addrs = append(addrs, v.OperatorAddress)
	}
	return addrs
}

func getOutstandingRewards(addr string, valAddr string) sdk.DecCoins {
	endpoint := addr + fmt.Sprintf(outstandingRewardsEndpoint, valAddr)
	res, err := http.Get(endpoint)
	if err != nil {
		panic(err)
	}

	var response distributiontypes.QueryValidatorOutstandingRewardsResponse
	bz, err := io.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}

	err = json.Unmarshal(bz, &response)
	if err != nil {
		fmt.Println("failed to query rewards for ", valAddr, " on ", addr, " with error: ", err)
		return sdk.DecCoins{}
	}

	return response.Rewards.Rewards
}

// QueryDenomResponse is the response type for the Query/Denom RPC
// method.
type QueryDenomResponse struct {
	// denom returns the requested denomination.
	Denom *Denom `protobuf:"bytes,1,opt,name=denom,proto3" json:"denom,omitempty"`
}

// Denom holds the base denom of a Token and a trace of the chains it was sent through.
type Denom struct {
	// the base token denomination
	Base string `protobuf:"bytes,1,opt,name=base,proto3" json:"base,omitempty"`
}

func getDenom(addr string, denom string) string {
	endpoint := addr + fmt.Sprintf(ibcDenomsEndpoint, denom)
	res, err := http.Get(endpoint)
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()
	var response QueryDenomResponse
	bz, err := io.ReadAll(res.Body)
	if err != nil {
		return ""
	}
	err = json.Unmarshal(bz, &response)
	if err != nil {
		return ""
	}
	if response.Denom == nil {
		return ""
	} else if response.Denom.Base == "" {
		return ""
	}
	return response.Denom.Base
}
