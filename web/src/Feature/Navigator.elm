module Feature.Navigator exposing (Config, SavedItem, view)

import Domain.Collection exposing (Collection)
import Html exposing (Html, aside, button, div, input, li, p, span, text, ul)
import Html.Attributes exposing (attribute, class, classList, disabled, placeholder, title, type_, value)
import Html.Events exposing (onClick, onInput)
import Set exposing (Set)
import Ui.Panel as Panel exposing (Presentation)


{-| A saved search row. `id` is the server id, or the query itself when
the on-device fallback list is active; `label` is the query to run.
-}
type alias SavedItem =
    { id : String
    , label : String
    }


type alias Config msg =
    { presentation : Presentation
    , current : String
    , saved : List SavedItem
    , savedStatus : Maybe String
    , savedSaving : Bool
    , localFallback : Bool
    , recent : List String
    , collections : List Collection
    , activeCollection : Maybe String
    , collectionDraft : String
    , onCollectionDraft : String -> msg
    , onCreateCollection : String -> msg
    , onSelectCollection : String -> msg
    , onOpenCollection : String -> msg
    , onAllPosts : msg
    , onMoveCollectionPost : String -> Int -> msg
    , onRemoveCollectionPost : String -> msg
    , removingPosts : Set String
    , onRun : String -> msg
    , onSave : msg
    , onRemove : String -> msg
    , onClose : msg
    }


view : Config msg -> Html msg
view config =
    let
        canSave =
            String.trim config.current /= "" && not (List.any (\item -> item.label == config.current) config.saved) && not config.savedSaving

        active =
            config.activeCollection
                |> Maybe.andThen (\id -> List.filter (\c -> c.id == id) config.collections |> List.head)
    in
    aside [ class (Panel.presentationClass "navigator" config.presentation), attribute "aria-label" "Navigator" ]
        [ Panel.header "Library" config.presentation config.onClose
        , div [ class "panel-body" ]
            [ Panel.section "Saved searches"
                [ if List.isEmpty config.saved then
                    p [ class "panel-note" ] [ text "Save a search to keep it here." ]

                  else
                    ul [ class "nav-list" ]
                        (List.map
                            (\item ->
                                li [ class "nav-row" ]
                                    [ queryButton config item.label
                                    , button
                                        [ class "nav-remove"
                                        , type_ "button"
                                        , onClick (config.onRemove item.id)
                                        , disabled config.savedSaving
                                        , attribute "aria-label" ("Remove saved search " ++ item.label)
                                        ]
                                        [ text "×" ]
                                    ]
                            )
                            config.saved
                        )
                , case config.savedStatus of
                    Just status ->
                        p [ class "panel-note" ] [ text status ]

                    Nothing ->
                        text ""
                , if config.localFallback then
                    p [ class "panel-note" ] [ text "Saved on this device." ]

                  else
                    text ""
                , button [ class "button button-quiet nav-save", type_ "button", onClick config.onSave, disabled (not canSave) ]
                    [ text "Save current search" ]
                ]
            , Panel.section "Recent"
                [ if List.isEmpty config.recent then
                    p [ class "panel-note" ] [ text "Searches from this session appear here." ]

                  else
                    ul [ class "nav-list" ] (List.map (\query -> li [ class "nav-row" ] [ queryButton config query ]) config.recent)
                ]
            , Panel.section "Collections"
                [ input [ class "nav-input", type_ "text", placeholder "New collection", value config.collectionDraft, onInput config.onCollectionDraft ] []
                , button [ class "button button-quiet nav-save", type_ "button", onClick (config.onCreateCollection config.collectionDraft), disabled (String.trim config.collectionDraft == "") ]
                    [ text "Create collection" ]
                , button [ class "button button-quiet nav-save", type_ "button", onClick config.onAllPosts ]
                    [ text "All posts" ]
                , if List.isEmpty config.collections then
                    p [ class "panel-note" ] [ text "Create a collection for selected posts." ]

                  else
                    ul [ class "nav-list" ]
                        (List.map
                            (\collection ->
                                li [ class "nav-row" ]
                                        [ button
                                        [ class "nav-item"
                                        , classList [ ( "is-current", Just collection.id == config.activeCollection ) ]
                                        , type_ "button"
                                        , onClick (config.onSelectCollection collection.id)
                                        ]
                                        [ span [] [ text (collection.name ++ " (" ++ String.fromInt (List.length collection.postIds) ++ ")") ] ]
                                    , button
                                        [ class "button button-quiet nav-save"
                                        , type_ "button"
                                        , onClick (config.onOpenCollection collection.id)
                                        , attribute "aria-label" ("Open collection " ++ collection.name)
                                        ]
                                        [ text "Open" ]
                                    ]
                            )
                            config.collections
                        )
                , case active of
                    Nothing ->
                        text ""

                    Just collection ->
                        if List.isEmpty collection.postIds then
                            p [ class "panel-note" ] [ text "No posts in this collection." ]

                        else
                            div [ class "collection-order" ]
                                [ p [ class "panel-label" ] [ text (collection.name ++ " order") ]
                                , ul [ class "nav-list collection-order-list" ]
                                    (List.indexedMap
                                        (\idx postId ->
                                            let
                                                isFirst =
                                                    idx == 0

                                                isLast =
                                                    idx == List.length collection.postIds - 1
                                            in
                                            li [ class "nav-row collection-order-row" ]
                                                [ span [ class "collection-order-id", title postId ] [ text ("#" ++ postId) ]
                                                , span [ class "collection-order-spacer" ] []
                                                , button [ class "collection-move", type_ "button", disabled isFirst, onClick (config.onMoveCollectionPost postId -1), attribute "aria-label" ("Move #" ++ postId ++ " up") ] [ text "↑" ]
                                                , button [ class "collection-move", type_ "button", disabled isLast, onClick (config.onMoveCollectionPost postId 1), attribute "aria-label" ("Move #" ++ postId ++ " down") ] [ text "↓" ]
                                                , button [ class "collection-remove", type_ "button", disabled (Set.member postId config.removingPosts), onClick (config.onRemoveCollectionPost postId), attribute "aria-label" ("Remove #" ++ postId ++ " from collection") ] [ text "×" ]
                                                ]
                                        )
                                        collection.postIds
                                    )
                                ]
                ]
            ]
        ]


queryButton : Config msg -> String -> Html msg
queryButton config query =
    button
        [ class "nav-item"
        , classList [ ( "is-current", query == config.current ) ]
        , type_ "button"
        , title query
        , onClick (config.onRun query)
        ]
        [ span [] [ text query ] ]
