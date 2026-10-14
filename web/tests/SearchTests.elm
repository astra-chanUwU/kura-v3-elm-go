module SearchTests exposing (allPassed, suite)

import Domain.Post exposing (PostSummary)
import Domain.Rating
import Feature.Search as Search
import Http


ctx : Search.Context
ctx =
    Search.context ""


startModel : String -> Search.Model
startModel query =
    Search.start ctx query Search.init |> Tuple.first


settle : Search.Msg -> Search.Model -> ( Search.Model, List Search.OutMsg )
settle msg model =
    let
        ( settled, _, outs ) =
            Search.update ctx msg model
    in
    ( settled, outs )


paginate : Search.Model -> Search.Model
paginate model =
    Search.requestMore ctx model |> Tuple.first


started : Search.Model
started =
    startModel "cats"


caseStartSubmits : Bool
caseStartSubmits =
    Search.query started == "cats" && Search.state started == Search.Searching && Search.nextCursor started == Nothing


caseStartBumpsGeneration : Bool
caseStartBumpsGeneration =
    Search.requestId started == Search.requestId Search.init + 1


caseStaleInitialIgnored : Bool
caseStaleInitialIgnored =
    let
        response =
            { posts = [], nextCursor = Just "c1" }

        ( settled, outs ) =
            settle (Search.Completed 999 Search.InitialRequest (Ok response)) started
    in
    outs == [] && settled == started


caseStaleMoreIgnored : Bool
caseStaleMoreIgnored =
    let
        ( settled, outs ) =
            settle (Search.Completed 999 Search.MoreRequest (Err Http.Timeout)) started
    in
    outs == [] && settled == started


caseInitialArrives : Bool
caseInitialArrives =
    let
        response =
            { posts = [], nextCursor = Just "c1" }

        ( settled, outs ) =
            settle (Search.Completed (Search.requestId started) Search.InitialRequest (Ok response)) started
    in
    outs == [ Search.InitialArrived response ] && Search.state settled == Search.Ready && Search.nextCursor settled == Just "c1"


caseMoreArrives : Bool
caseMoreArrives =
    let
        first =
            { posts = [], nextCursor = Just "c1" }

        second =
            { posts = [], nextCursor = Nothing }

        ( ready, _ ) =
            settle (Search.Completed (Search.requestId started) Search.InitialRequest (Ok first)) started

        ( settled, outs ) =
            settle (Search.Completed (Search.requestId ready) Search.MoreRequest (Ok second)) (paginate ready)
    in
    outs == [ Search.MoreArrived second ] && Search.state settled == Search.Ready && Search.nextCursor settled == Nothing


caseFailureReported : Bool
caseFailureReported =
    let
        ( settled, outs ) =
            settle (Search.Completed (Search.requestId started) Search.InitialRequest (Err Http.Timeout)) started
    in
    outs == [ Search.SearchFailed Http.Timeout ] && Search.state settled == Search.Failed "The search timed out."


caseErrorTextRetainsBadUrlAndStatus : Bool
caseErrorTextRetainsBadUrlAndStatus =
    Search.errorToString (Http.BadUrl "https://bad.invalid") == "Invalid API URL: https://bad.invalid"
        && Search.errorToString (Http.BadStatus 409) == "Search failed (HTTP 409)."


caseRequestMoreWithoutCursorNoop : Bool
caseRequestMoreWithoutCursorNoop =
    paginate started == started


caseRequestMoreEntersLoading : Bool
caseRequestMoreEntersLoading =
    let
        ( ready, _ ) =
            settle (Search.Completed (Search.requestId started) Search.InitialRequest (Ok { posts = [], nextCursor = Just "c1" })) started

        paged =
            paginate ready
    in
    Search.state paged == Search.LoadingMore && Search.nextCursor paged == Just "c1"


caseResetClears : Bool
caseResetClears =
    let
        cleared =
            Search.reset started
    in
    Search.query cleared == "" && Search.state cleared == Search.Idle && Search.nextCursor cleared == Nothing


caseRestartResubmits : Bool
caseRestartResubmits =
    let
        ( ready, _ ) =
            settle (Search.Completed (Search.requestId started) Search.InitialRequest (Ok { posts = [], nextCursor = Nothing })) started

        restarted =
            Search.restart ctx ready |> Tuple.first
    in
    Search.state restarted == Search.Searching && Search.query restarted == "cats" && Search.nextCursor restarted == Nothing


summary : String -> PostSummary
summary id =
    { id = id, previewUrl = "/m", originalUrl = "/o", mediaType = "image/jpeg", width = 1, height = 1, tags = [], rating = Domain.Rating.Unrated, ratingVersion = Just 0 }


prefixCount : List Search.OutMsg -> Int
prefixCount outs =
    case outs of
        [ Search.PrefixArrived response ] ->
            List.length response.posts

        _ ->
            0


isSearchFailed : List Search.OutMsg -> Bool
isSearchFailed outs =
    case outs of
        [ Search.SearchFailed _ ] ->
            True

        _ ->
            False


readyModel : Search.Model
readyModel =
    let
        ( ready, _ ) =
            settle (Search.Completed (Search.requestId started) Search.InitialRequest (Ok { posts = List.map summary (List.range 1 60 |> List.map String.fromInt), nextCursor = Just "c1" })) started
    in
    ready


casePrefixMultiPage : Bool
casePrefixMultiPage =
    let
        refreshing =
            Search.refreshPrefix ctx 120 readyModel |> Tuple.first

        firstPage =
            { posts = List.map summary (List.range 1 60 |> List.map String.fromInt), nextCursor = Just "c1" }

        secondPage =
            { posts = List.map summary (List.range 61 120 |> List.map String.fromInt), nextCursor = Just "c2" }

        ( afterFirst, outs1 ) =
            settle (Search.Completed (Search.requestId refreshing) Search.PrefixRequest (Ok firstPage)) refreshing

        ( afterSecond, outs2 ) =
            settle (Search.Completed (Search.requestId afterFirst) Search.PrefixRequest (Ok secondPage)) afterFirst
    in
    outs1 == [] && prefixCount outs2 == 120 && Search.state afterSecond == Search.Ready


casePrefixCancelledByNewSearch : Bool
casePrefixCancelledByNewSearch =
    let
        refreshing =
            Search.refreshPrefix ctx 60 readyModel |> Tuple.first

        ( restarted, _ ) =
            ( Search.start ctx "dogs" refreshing |> Tuple.first, () )

        ( settled, outs ) =
            settle (Search.Completed (Search.requestId refreshing) Search.PrefixRequest (Ok { posts = [], nextCursor = Nothing })) restarted
    in
    outs == [] && settled == restarted


casePrefixRepeatedCursor : Bool
casePrefixRepeatedCursor =
    let
        refreshing =
            Search.refreshPrefix ctx 120 readyModel |> Tuple.first

        firstPage =
            { posts = List.map summary (List.range 1 60 |> List.map String.fromInt), nextCursor = Just "c1" }

        repeated =
            { posts = List.map summary (List.range 61 120 |> List.map String.fromInt), nextCursor = Just "c1" }

        ( afterFirst, _ ) =
            settle (Search.Completed (Search.requestId refreshing) Search.PrefixRequest (Ok firstPage)) refreshing

        ( settled, outs ) =
            settle (Search.Completed (Search.requestId afterFirst) Search.PrefixRequest (Ok repeated)) afterFirst
    in
    isSearchFailed outs && Search.state settled == Search.Ready && Search.nextCursor settled == Just "c1"


casePrefixFailureKeepsReady : Bool
casePrefixFailureKeepsReady =
    let
        refreshing =
            Search.refreshPrefix ctx 60 readyModel |> Tuple.first

        ( settled, outs ) =
            settle (Search.Completed (Search.requestId refreshing) Search.PrefixRequest (Err Http.Timeout)) refreshing
    in
    Search.state settled == Search.Ready && Search.nextCursor settled == Just "c1" && isSearchFailed outs


casePrefixStaleIgnored : Bool
casePrefixStaleIgnored =
    let
        refreshing =
            Search.refreshPrefix ctx 60 readyModel |> Tuple.first

        ( settled, outs ) =
            settle (Search.Completed 999 Search.PrefixRequest (Ok { posts = [], nextCursor = Nothing })) refreshing
    in
    outs == [] && settled == refreshing


casePrefixCursorAlignment : Bool
casePrefixCursorAlignment =
    let
        refreshing =
            Search.refreshPrefix ctx 121 readyModel |> Tuple.first

        page start end cursor =
            { posts = List.map (String.fromInt >> summary) (List.range start end), nextCursor = Just cursor }

        ( first, _ ) =
            settle (Search.Completed (Search.requestId refreshing) Search.PrefixRequest (Ok (page 1 60 "c60"))) refreshing

        ( second, _ ) =
            settle (Search.Completed (Search.requestId first) Search.PrefixRequest (Ok (page 61 120 "c120"))) first

        ( completed, outs ) =
            settle (Search.Completed (Search.requestId second) Search.PrefixRequest (Ok (page 121 121 "c121"))) second

        ( _, appended ) =
            settle (Search.Completed (Search.requestId completed) Search.MoreRequest (Ok (page 122 180 "c180"))) (paginate completed)
    in
    Search.prefixPageLimit 121 120 == 1
        && Search.prefixPageLimit 1000 960 == 40
        && Search.prefixPageLimit 5 0 == 5
        && prefixCount outs == 121
        && Search.nextCursor completed == Just "c121"
        && appended == [ Search.MoreArrived (page 122 180 "c180") ]


casePrefixDuplicateGuards : Bool
casePrefixDuplicateGuards =
    let
        refreshing =
            Search.refreshPrefix ctx 120 readyModel |> Tuple.first

        ( samePage, sameOuts ) =
            settle (Search.Completed (Search.requestId refreshing) Search.PrefixRequest (Ok { posts = [ summary "1", summary "1" ], nextCursor = Just "c60" })) refreshing

        ( first, _ ) =
            settle (Search.Completed (Search.requestId refreshing) Search.PrefixRequest (Ok { posts = List.map (String.fromInt >> summary) (List.range 1 60), nextCursor = Just "c60" })) refreshing

        ( overlap, overlapOuts ) =
            settle (Search.Completed (Search.requestId first) Search.PrefixRequest (Ok { posts = [ summary "60", summary "61" ], nextCursor = Just "c61" })) first
    in
    isSearchFailed sameOuts
        && isSearchFailed overlapOuts
        && Search.nextCursor samePage == Just "c1"
        && Search.nextCursor overlap == Just "c1"
        && Search.state overlap == Search.Ready


casePrefixIgnoresUnrequestedExtraRows : Bool
casePrefixIgnoresUnrequestedExtraRows =
    let
        refreshing =
            Search.refreshPrefix ctx 1 readyModel |> Tuple.first

        ( settled, outs ) =
            settle (Search.Completed (Search.requestId refreshing) Search.PrefixRequest (Ok { posts = [ summary "1", summary "2" ], nextCursor = Just "c2" })) refreshing
    in
    isSearchFailed outs && Search.nextCursor settled == Just "c1"


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "start submits the query as searching", caseStartSubmits )
    , ( "start bumps the request generation", caseStartBumpsGeneration )
    , ( "stale initial completion ignored", caseStaleInitialIgnored )
    , ( "stale follow-up completion ignored", caseStaleMoreIgnored )
    , ( "initial page arrives ready with cursor", caseInitialArrives )
    , ( "next page arrives ready with cursor", caseMoreArrives )
    , ( "failure reports and keeps the message", caseFailureReported )
    , ( "search error text preserves URLs and status codes", caseErrorTextRetainsBadUrlAndStatus )
    , ( "pagination without a cursor is a no-op", caseRequestMoreWithoutCursorNoop )
    , ( "pagination with a cursor loads more", caseRequestMoreEntersLoading )
    , ( "reset clears back to idle", caseResetClears )
    , ( "restart resubmits the current query", caseRestartResubmits )
    , ( "prefix refresh publishes only after all pages", casePrefixMultiPage )
    , ( "prefix refresh cancelled by a new search", casePrefixCancelledByNewSearch )
    , ( "prefix refresh repeated cursor publishes nothing", casePrefixRepeatedCursor )
    , ( "prefix refresh failure keeps ready state", casePrefixFailureKeepsReady )
    , ( "prefix refresh cursor includes every published row at partial page boundary", casePrefixCursorAlignment )
    , ( "prefix refresh duplicate and overlap pages publish nothing", casePrefixDuplicateGuards )
    , ( "prefix refresh oversized server page cannot lose cursor rows", casePrefixIgnoresUnrequestedExtraRows )
    , ( "prefix refresh stale generation ignored", casePrefixStaleIgnored )
    ]
